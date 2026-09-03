// Live keeps the dashboard's in-memory picture of the SoR: the KPI tree
// built from alert rules, a short window of recent samples per KPI for
// sparklines, which rules are firing, and a fan-out of updates to browser
// sessions. One gRPC watch feeds every browser.
import type { SamplePoint, SorClient, WatchEvent } from "./sor.js";
import { buildTree, kpiStatus, type Kpi, type KpiSeverity, type Namespace } from "./status.js";

export interface LiveUpdate {
  path: string;
  time: number;
  value: number;
  raw: string;
  unit: string;
  severity: KpiSeverity;
  alert?: "fired" | "resolved";
}

export interface LiveOptions {
  /** Samples kept per KPI for sparklines. */
  history?: number;
  /** Delay before re-opening the watch after it drops. */
  reconnectMs?: number;
  log?: (msg: string) => void;
}

export class Live {
  tree: Namespace[] = [];
  /** Why the SoR could not be reached, until it answers again. */
  lastError?: string;
  private kpis = new Map<string, Kpi>();
  private window = new Map<string, SamplePoint[]>();
  private firingRules = new Set<string>(); // "path|op|threshold" per firing rule
  private listeners = new Set<(u: LiveUpdate) => void>();
  private readonly history: number;
  private readonly reconnectMs: number;
  private readonly log: (msg: string) => void;
  private abort?: AbortController;

  constructor(private sor: SorClient, opts: LiveOptions = {}) {
    this.history = opts.history ?? 60;
    this.reconnectMs = opts.reconnectMs ?? 2000;
    this.log = opts.log ?? (() => {});
  }

  /** Loads rules and seeds the recent window of every KPI from the SoR. */
  async load(): Promise<void> {
    let states;
    try {
      states = await this.sor.rules();
    } catch (err) {
      this.lastError = err instanceof Error ? err.message : String(err);
      throw err;
    }
    this.lastError = undefined;
    this.tree = buildTree(states.map((s) => s.rule));
    this.kpis.clear();
    for (const ns of this.tree) for (const svc of ns.services) for (const kpi of svc.kpis) this.kpis.set(kpi.path, kpi);
    this.firingRules.clear();
    for (const s of states) if (s.firing) this.firingRules.add(ruleKey(s.rule));
    await Promise.all(
      [...this.kpis.keys()].map(async (path) => {
        const samples = await this.sor.cat(path).catch(() => [] as SamplePoint[]);
        this.window.set(path, samples.slice(-this.history));
      }),
    );
  }

  /** Keeps the watch open, reconnecting until stop() is called. */
  async run(): Promise<void> {
    this.abort = new AbortController();
    const { signal } = this.abort;
    while (!signal.aborted) {
      try {
        for await (const ev of this.sor.watch(signal)) this.handle(ev);
      } catch (err) {
        if (signal.aborted) return;
        this.lastError = err instanceof Error ? err.message : String(err);
        this.log(`watch dropped: ${this.lastError}`);
      }
      if (signal.aborted) return;
      await new Promise((r) => setTimeout(r, this.reconnectMs));
      try {
        await this.load(); // rules or state may have changed while away
      } catch (err) {
        this.log(`reload failed: ${err instanceof Error ? err.message : err}`);
      }
    }
  }

  stop(): void {
    this.abort?.abort();
  }

  kpi(path: string): Kpi | undefined {
    return this.kpis.get(path);
  }

  recent(path: string): SamplePoint[] {
    return this.window.get(path) ?? [];
  }

  latest(path: string): SamplePoint | undefined {
    return this.recent(path).at(-1);
  }

  status(path: string): KpiSeverity {
    const kpi = this.kpis.get(path);
    if (!kpi) return "unknown";
    return kpiStatus(kpi, this.latest(path)?.value, this.firingPaths());
  }

  firingPaths(): Set<string> {
    const out = new Set<string>();
    for (const key of this.firingRules) out.add(key.slice(0, key.indexOf("|")));
    return out;
  }

  onUpdate(listener: (u: LiveUpdate) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** Applies one event from the watch stream. Exported for tests. */
  handle(ev: WatchEvent): void {
    const kpi = this.kpis.get(ev.path);
    if (!kpi) return;
    if (ev.alert) {
      const key = ruleKey(ev.alert.rule);
      if (ev.alert.state === "fired") this.firingRules.add(key);
      else this.firingRules.delete(key);
    } else {
      const win = this.window.get(ev.path) ?? [];
      win.push(ev.sample);
      if (win.length > this.history) win.splice(0, win.length - this.history);
      this.window.set(ev.path, win);
    }
    const update: LiveUpdate = {
      path: ev.path,
      time: ev.sample.time,
      value: ev.sample.value,
      raw: ev.sample.raw,
      unit: ev.sample.unit,
      severity: this.status(ev.path),
    };
    if (ev.alert) update.alert = ev.alert.state;
    for (const l of this.listeners) l(update);
  }
}

function ruleKey(r: { path: string; op: string; threshold: number }): string {
  return `${r.path}|${r.op}|${r.threshold}`;
}
