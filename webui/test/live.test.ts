import { describe, expect, it } from "vitest";
import { Live, type LiveUpdate } from "../src/live.js";
import type { RuleState, SamplePoint, SorClient, WatchEvent } from "../src/sor.js";
import type { Rule } from "../src/status.js";

const mem: Rule = { path: "/iam/iam-api/metrics/go.memory.used.dat", op: ">", threshold: 100, holdForSeconds: 60, channel: "ops" };
const cpu: Rule = { path: "/iam/iam-api/metrics/process.cpu.utilization.dat", op: ">=", threshold: 0.8, holdForSeconds: 30, channel: "ops" };

const pt = (time: number, value: number, unit = "By"): SamplePoint => ({ time, value, raw: String(value), unit });

function fakeSor(rules: RuleState[], history: Record<string, SamplePoint[]>): SorClient {
  return {
    rules: async () => rules,
    cat: async (path) => history[path] ?? [],
    async *watch() {},
  };
}

describe("Live", () => {
  it("seeds the tree, recent samples and firing state from the SoR", async () => {
    const live = new Live(fakeSor([{ rule: mem, firing: true }, { rule: cpu, firing: false }], {
      [mem.path]: [pt(1, 10), pt(2, 20), pt(3, 30)],
    }), { history: 2 });
    await live.load();
    expect(live.tree.map((n) => n.name)).toEqual(["iam"]);
    expect(live.recent(mem.path)).toEqual([pt(2, 20), pt(3, 30)]);
    expect(live.recent(cpu.path)).toEqual([]);
    expect(live.status(mem.path)).toBe("red"); // firing per the SoR
    expect(live.status(cpu.path)).toBe("unknown");
  });

  it("applies sample events, keeps a bounded window and notifies listeners", async () => {
    const live = new Live(fakeSor([{ rule: mem, firing: false }], {}), { history: 2 });
    await live.load();
    const seen: LiveUpdate[] = [];
    const off = live.onUpdate((u) => seen.push(u));

    live.handle({ path: mem.path, sample: pt(1, 50) });
    live.handle({ path: mem.path, sample: pt(2, 80) });
    live.handle({ path: mem.path, sample: pt(3, 150) });
    live.handle({ path: "/iam/iam-api/metrics/other.dat", sample: pt(3, 1) }); // not a kpi

    expect(live.recent(mem.path)).toEqual([pt(2, 80), pt(3, 150)]);
    expect(seen.map((u) => u.severity)).toEqual(["green", "green", "amber"]);
    expect(seen[2]).toMatchObject({ path: mem.path, time: 3, value: 150, raw: "150", unit: "By" });

    off();
    live.handle({ path: mem.path, sample: pt(4, 1) });
    expect(seen).toHaveLength(3);
  });

  it("tracks firing from alert events", async () => {
    const live = new Live(fakeSor([{ rule: mem, firing: false }], {}), { history: 5 });
    await live.load();
    const seen: LiveUpdate[] = [];
    live.onUpdate((u) => seen.push(u));

    // The SoR publishes the sample first, then the alert it triggered.
    live.handle({ path: mem.path, sample: pt(5, 200) });
    const fired: WatchEvent = { path: mem.path, sample: pt(5, 200), alert: { rule: mem, state: "fired" } };
    live.handle(fired);
    expect(live.status(mem.path)).toBe("red");
    expect(seen.at(-1)).toMatchObject({ alert: "fired", severity: "red" });

    live.handle({ path: mem.path, sample: pt(6, 10) });
    live.handle({ path: mem.path, sample: pt(6, 10), alert: { rule: mem, state: "resolved" } });
    expect(live.status(mem.path)).toBe("green");
    expect(seen.at(-1)).toMatchObject({ alert: "resolved", severity: "green" });
    // Alert events carry the triggering sample but must not double-append it.
    expect(live.recent(mem.path).map((p) => p.time)).toEqual([5, 6]);
  });
});

describe("Live connectivity", () => {
  it("records the last error and clears it once the SoR answers", async () => {
    let up = false;
    const sor: SorClient = {
      rules: async () => { if (!up) throw new Error("connect ECONNREFUSED"); return [{ rule: mem, firing: false }]; },
      cat: async () => [],
      async *watch() {},
    };
    const live = new Live(sor);
    await expect(live.load()).rejects.toThrow("ECONNREFUSED");
    expect(live.lastError).toContain("ECONNREFUSED");
    up = true;
    await live.load();
    expect(live.lastError).toBeUndefined();
    expect(live.tree).toHaveLength(1);
  });
});
