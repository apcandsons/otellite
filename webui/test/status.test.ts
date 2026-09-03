import { describe, expect, it } from "vitest";
import { breaches, buildTree, kpiStatus, parsePath, rollup, worst, type Rule } from "../src/status.js";

const mem: Rule = { path: "/iam/iam-api/metrics/go.memory.used.dat", op: ">", threshold: 100, holdForSeconds: 60, channel: "ops" };
const cpu: Rule = { path: "/iam/iam-api/metrics/process.cpu.utilization.dat", op: ">=", threshold: 0.8, holdForSeconds: 30, channel: "ops" };
const uptime: Rule = { path: "/iam/iam-api/metrics/process.uptime.dat", op: "<", threshold: 10, holdForSeconds: 60, channel: "ops" };
const web: Rule = { path: "/web/frontend/metrics/http.server.errors.dat", op: ">", threshold: 500, holdForSeconds: 5, channel: "ops" };

describe("parsePath", () => {
  it("splits a metrics .dat path", () => {
    expect(parsePath(mem.path)).toEqual({ namespace: "iam", service: "iam-api", name: "go.memory.used" });
  });
  it("rejects anything that is not a metrics stream", () => {
    expect(() => parsePath("/iam/iam-api/logs/app.dat")).toThrow();
    expect(() => parsePath("/iam/iam-api")).toThrow();
  });
});

describe("breaches", () => {
  it("is true when the rule's condition holds on the value", () => {
    expect(breaches(101, mem)).toBe(true);
    expect(breaches(100, mem)).toBe(false); // strict >
    expect(breaches(0.8, cpu)).toBe(true);
    expect(breaches(9, uptime)).toBe(true);
    expect(breaches(10, uptime)).toBe(false);
  });
  it("orders red > amber > green > unknown", () => {
    expect(worst(["green", "amber"])).toBe("amber");
    expect(worst(["amber", "red", "green"])).toBe("red");
    expect(worst(["unknown", "green"])).toBe("green");
    expect(worst([])).toBe("green");
  });
});

describe("buildTree", () => {
  it("groups rules by namespace, service and stream, keeping order", () => {
    const tree = buildTree([mem, cpu, web, uptime]);
    expect(tree.map((n) => n.name)).toEqual(["iam", "web"]);
    const iam = tree[0];
    expect(iam.services.map((s) => s.name)).toEqual(["iam-api"]);
    expect(iam.services[0].kpis.map((k) => k.name)).toEqual(["go.memory.used", "process.cpu.utilization", "process.uptime"]);
    expect(iam.services[0].kpis[0]).toMatchObject({ path: mem.path, namespace: "iam", service: "iam-api", rules: [mem] });
  });
  it("attaches several rules on one stream to a single kpi", () => {
    const tight = { ...mem, threshold: 50 };
    const tree = buildTree([mem, tight]);
    expect(tree[0].services[0].kpis).toHaveLength(1);
    expect(tree[0].services[0].kpis[0].rules).toEqual([mem, tight]);
  });
});

describe("kpiStatus", () => {
  const kpi = buildTree([mem, { ...mem, threshold: 50 }])[0].services[0].kpis[0];
  it("is amber when any rule's threshold is crossed but nothing has fired yet", () => {
    expect(kpiStatus(kpi, 60, new Set())).toBe("amber");
    expect(kpiStatus(kpi, 150, new Set())).toBe("amber");
  });
  it("is red once a rule on the stream is firing, whatever the latest value", () => {
    expect(kpiStatus(kpi, 150, new Set([mem.path]))).toBe("red");
    expect(kpiStatus(kpi, 10, new Set([mem.path]))).toBe("red"); // fired, not yet resolved
    expect(kpiStatus(kpi, undefined, new Set([mem.path]))).toBe("red");
  });
  it("is green within threshold and unknown with no value yet", () => {
    expect(kpiStatus(kpi, 10, new Set())).toBe("green");
    expect(kpiStatus(kpi, undefined, new Set())).toBe("unknown");
  });
});

describe("rollup", () => {
  it("counts red and amber kpis for badges", () => {
    expect(rollup(["red", "red", "amber", "green", "unknown"])).toEqual({ red: 2, amber: 1, severity: "red" });
    expect(rollup(["green"])).toEqual({ red: 0, amber: 0, severity: "green" });
  });
});

describe("pressure", () => {
  it("is value over threshold for upward rules and the inverse for downward", async () => {
    const { pressure } = await import("../src/status.js");
    expect(pressure(50, mem)).toBe(0.5);
    expect(pressure(200, mem)).toBe(2);
    expect(pressure(20, uptime)).toBe(0.5); // 10 / 20
    expect(pressure(5, uptime)).toBe(2);
  });
  it("takes the highest pressure across a kpi's rules and clamps for tile sizing", async () => {
    const { kpiWeight } = await import("../src/status.js");
    const kpi = buildTree([mem, { ...mem, threshold: 50 }])[0].services[0].kpis[0];
    expect(kpiWeight(kpi, 40)).toBe(0.8);
    expect(kpiWeight(kpi, 1000)).toBe(4); // clamped
    expect(kpiWeight(kpi, 1)).toBe(0.25); // clamped
    expect(kpiWeight(kpi, undefined)).toBe(1); // unknown sits in the middle
  });
});

describe("absent rules", () => {
  const absent: Rule = { path: cpu.path, op: "absent", threshold: 0, holdForSeconds: 30, channel: "ops" };
  it("never breach on a value and carry neutral pressure", async () => {
    const { pressure } = await import("../src/status.js");
    expect(breaches(0.99, absent)).toBe(false);
    expect(pressure(0.99, absent)).toBe(1);
  });
  it("go red only through the firing state", () => {
    const kpi = buildTree([absent])[0].services[0].kpis[0];
    expect(kpiStatus(kpi, 0.99, new Set())).toBe("green");
    expect(kpiStatus(kpi, undefined, new Set())).toBe("unknown");
    expect(kpiStatus(kpi, undefined, new Set([absent.path]))).toBe("red");
  });
});
