// Pure dashboard logic: how alert rules become a namespace/service/KPI
// tree and how a value maps to red, amber or green. No I/O here.

export type Op = ">" | ">=" | "<" | "<=" | "absent";

export interface Rule {
  path: string;
  op: Op;
  threshold: number;
  holdForSeconds: number;
  channel: string;
}

export interface Kpi {
  path: string;
  namespace: string;
  service: string;
  name: string;
  rules: Rule[];
}

export interface Service {
  namespace: string;
  name: string;
  kpis: Kpi[];
}

export interface Namespace {
  name: string;
  services: Service[];
}

/**
 * KPI colour: red when a rule on the stream is firing (its condition has
 * held for the rule's duration), amber when the latest value crosses a
 * threshold but no rule has fired yet, green within thresholds, unknown
 * with no data.
 */
export type KpiSeverity = "red" | "amber" | "green" | "unknown";

export interface Rollup {
  red: number;
  amber: number;
  severity: KpiSeverity;
}

export function parsePath(path: string): { namespace: string; service: string; name: string } {
  const m = /^\/([^/]+)\/([^/]+)\/metrics\/([^/]+)\.dat$/.exec(path);
  if (!m) throw new Error(`${path} is not a metrics stream path`);
  return { namespace: m[1], service: m[2], name: m[3] };
}

export function holds(op: Op, value: number, threshold: number): boolean {
  switch (op) {
    case ">": return value > threshold;
    case ">=": return value >= threshold;
    case "<": return value < threshold;
    case "<=": return value <= threshold;
    case "absent": return false; // evaluated by the SoR on a clock, surfaced via firing state
  }
}

/** Whether the rule's condition holds on this value. */
export function breaches(value: number, rule: Rule): boolean {
  return holds(rule.op, value, rule.threshold);
}

const rank: Record<KpiSeverity, number> = { red: 3, amber: 2, green: 1, unknown: 0 };

export function worst(sevs: KpiSeverity[]): KpiSeverity {
  let out: KpiSeverity = "green";
  for (const s of sevs) if (rank[s] > rank[out]) out = s;
  return out;
}

/** Groups rules into namespaces, services and KPIs in first-seen order. */
export function buildTree(rules: Rule[]): Namespace[] {
  const tree: Namespace[] = [];
  for (const rule of rules) {
    const { namespace, service, name } = parsePath(rule.path);
    let ns = tree.find((n) => n.name === namespace);
    if (!ns) tree.push((ns = { name: namespace, services: [] }));
    let svc = ns.services.find((s) => s.name === service);
    if (!svc) ns.services.push((svc = { namespace, name: service, kpis: [] }));
    let kpi = svc.kpis.find((k) => k.path === rule.path);
    if (!kpi) svc.kpis.push((kpi = { path: rule.path, namespace, service, name, rules: [] }));
    kpi.rules.push(rule);
  }
  return tree;
}

export function kpiStatus(kpi: Kpi, value: number | undefined, firingPaths: Set<string>): KpiSeverity {
  if (firingPaths.has(kpi.path)) return "red";
  if (value === undefined || Number.isNaN(value)) return "unknown";
  return kpi.rules.some((r) => breaches(value, r)) ? "amber" : "green";
}

export function rollup(sevs: KpiSeverity[]): Rollup {
  return {
    red: sevs.filter((s) => s === "red").length,
    amber: sevs.filter((s) => s === "amber").length,
    severity: worst(sevs),
  };
}

/**
 * How close a value is to breaching: 1 means exactly at the threshold,
 * above 1 the rule holds. Upward rules divide value by threshold;
 * downward rules divide threshold by value.
 */
export function pressure(value: number, rule: Rule): number {
  if (rule.op === "absent") return 1;
  const upward = rule.op === ">" || rule.op === ">=";
  const [num, den] = upward ? [value, rule.threshold] : [rule.threshold, value];
  if (den === 0) return num === 0 ? 1 : Infinity;
  return Math.max(0, num / den);
}

export const WEIGHT_MIN = 0.25;
export const WEIGHT_MAX = 4;

/** Treemap weight for a KPI tile: the worst pressure across its rules, clamped. */
export function kpiWeight(kpi: Kpi, value: number | undefined): number {
  if (value === undefined || !Number.isFinite(value)) return 1;
  const p = Math.max(...kpi.rules.map((r) => pressure(value, r)));
  return Math.min(WEIGHT_MAX, Math.max(WEIGHT_MIN, p));
}
