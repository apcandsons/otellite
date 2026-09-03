import { describe, expect, it } from "vitest";
import { Live } from "../src/live.js";
import type { RuleState, SamplePoint, SorClient } from "../src/sor.js";
import type { Rule } from "../src/status.js";
import { HeatmapPage, HistoryPage, TabularPage } from "../src/views.js";

const mem: Rule = { path: "/iam/iam-api/metrics/go.memory.used.dat", op: ">", threshold: 100, holdForSeconds: 60, channel: "ops" };
const cpu: Rule = { path: "/iam/iam-api/metrics/process.cpu.utilization.dat", op: ">=", threshold: 0.8, holdForSeconds: 30, channel: "ops" };
const web: Rule = { path: "/web/frontend/metrics/http.server.errors.dat", op: ">", threshold: 500, holdForSeconds: 5, channel: "ops" };
const pt = (time: number, value: number, unit = "By"): SamplePoint => ({ time, value, raw: String(value), unit });

function sor(rules: RuleState[], history: Record<string, SamplePoint[]>): SorClient {
  return { rules: async () => rules, cat: async (p) => history[p] ?? [], async *watch() {} };
}

async function live(): Promise<Live> {
  const l = new Live(sor(
    [{ rule: mem, firing: true }, { rule: cpu, firing: false }, { rule: web, firing: false }],
    { [mem.path]: [pt(1, 50), pt(2, 150)], [cpu.path]: [pt(2, 0.85, "1")] }, // cpu crosses 0.8 but has not fired
  ));
  await l.load();
  return l;
}

describe("TabularPage", () => {
  it("lists namespaces with collapsed services, badges and sparklines", async () => {
    const html = (await TabularPage({ live: await live() })).toString();
    expect(html).toContain('data-ns="iam"');
    expect(html).toContain('data-ns="web"');
    // services are collapsed: their <details> carry no open attribute
    expect(html).toMatch(/<details class="svc"[^>]*data-svc="iam-api"(?![^>]*\bopen\b)/);
    // namespace-level badges roll up their services
    expect(html).toMatch(/data-ns="iam"[\s\S]*?class="badge red"[^>]*>1</);
    expect(html).toMatch(/data-ns="iam"[\s\S]*?class="badge amber"[^>]*>1</);
    expect(html).not.toContain("badge firing");
    // kpi rows link to history and carry a sparkline
    expect(html).toContain('href="/history/iam/iam-api/go.memory.used"');
    expect(html).toMatch(/<polyline[^>]*points="[^"]+"/);
    expect(html).toContain("150 B");
  });
});

describe("HeatmapPage", () => {
  it("colours tiles by severity and links each to history", async () => {
    const html = (await HeatmapPage({ live: await live() }))!.toString();
    expect(html).toMatch(/<a class="tile red[^"]*"[^>]*href="\/history\/iam\/iam-api\/go.memory.used"/);
    expect(html).toMatch(/<a class="tile amber[^"]*"[^>]*href="\/history\/iam\/iam-api\/process.cpu.utilization"/);
    expect(html).toMatch(/<a class="tile unknown[^"]*"[^>]*href="\/history\/web\/frontend\/http.server.errors"/);
  });
  it("lays namespaces, services and tiles out as a treemap with a legend", async () => {
    const html = (await HeatmapPage({ live: await live() }))!.toString();
    const pct = "left:[\\d.]+%;top:[\\d.]+%;width:[\\d.]+%;height:[\\d.]+%";
    expect(html).toMatch(new RegExp(`<section class="ns"[^>]*data-ns="iam"[^>]*style="${pct}"`));
    expect(html).toMatch(new RegExp(`<section class="svc"[^>]*data-svc="iam-api"[^>]*style="${pct}"`));
    expect(html).toMatch(new RegExp(`<a class="tile[^"]*"[^>]*data-path="${mem.path}"[^>]*style="${pct}"`));
    // the firing kpi (pressure 1.5) gets more area than the amber one (1.06)
    const areaOf = (path: string) => {
      const m = new RegExp(`data-path="${path}"[^>]*style="left:[\\d.]+%;top:[\\d.]+%;width:([\\d.]+)%;height:([\\d.]+)%"`).exec(html)!;
      return Number(m[1]) * Number(m[2]);
    };
    expect(areaOf(mem.path)).toBeGreaterThan(areaOf(cpu.path));
    expect(html).toContain('class="legend"');
    // one header per service, naming its namespace too; no separate namespace header row
    expect(html).toMatch(/<h3[^>]*><a class="ns-link" href="\/heatmap\/iam">iam<\/a><span class="sep">\/<\/span><a href="\/heatmap\/iam\/iam-api">iam-api<\/a>/);
    expect(html).not.toMatch(/<section class="ns"[^>]*>\s*<h2/);
  });
  it("narrows to a namespace or a service", async () => {
    const l = await live();
    const ns = (await HeatmapPage({ live: l, namespace: "iam" }))!.toString();
    expect(ns).toContain("iam-api");
    expect(ns).not.toContain("frontend");
    const svc = (await HeatmapPage({ live: l, namespace: "iam", service: "iam-api" }))!.toString();
    expect(svc).toContain("go.memory.used");
    expect(svc).not.toContain("frontend");
  });
  it("reports an unknown namespace", async () => {
    expect(await HeatmapPage({ live: await live(), namespace: "nope" })).toBeNull();
  });
});

describe("HistoryPage", () => {
  it("draws the whole series with a threshold line per rule", async () => {
    const l = await live();
    const html = (await HistoryPage({ live: l, kpi: l.kpi(mem.path)!, samples: [pt(1, 50), pt(2, 150), pt(3, 90)] })).toString();
    expect(html).toContain("go.memory.used");
    expect(html).toMatch(/<path class="series"[^>]*d="M/);
    expect(html).toMatch(/<line class="threshold"/);
    expect(html).toContain("&gt; 100");
    expect(html).toContain('data-path="/iam/iam-api/metrics/go.memory.used.dat"');
  });
});

describe("unreachable SoR", () => {
  it("says so instead of claiming there are no rules", async () => {
    const l = new Live({ rules: async () => { throw new Error("ECONNREFUSED 4319"); }, cat: async () => [], async *watch() {} });
    await l.load().catch(() => {});
    const html = (await TabularPage({ live: l })).toString();
    expect(html).toContain("ECONNREFUSED 4319");
    expect(html).not.toContain("No alert rules loaded");
  });
});

describe("stylesheet", () => {
  it("is emitted verbatim, not HTML-escaped", async () => {
    const html = (await TabularPage({ live: await live() })).toString();
    expect(html).toContain("details > summary");
    expect(html).not.toContain("&gt; summary");
  });
});

describe("HistoryChart gaps", () => {
  it("shades stretches without samples and anchors the axis at now", async () => {
    const { HistoryChart } = await import("../src/views.js");
    const l = await live();
    const kpi = l.kpi(mem.path)!;
    const samples = [0, 5000, 10000, 15000, 60000].map((t) => pt(t, 50));
    const html = (await HistoryChart({ kpi, samples, now: 120000 })).toString();
    expect(html.match(/<rect class="gap"/g)).toHaveLength(2); // 15s→60s and 60s→now
    expect(html).toContain("2 gaps without samples");
    expect(html).toMatch(/>now \d/);
  });
});

describe("modal detail", () => {
  it("renders the detail fragment without the page chrome", async () => {
    const { HistoryDetail } = await import("../src/views.js");
    const l = await live();
    const html = (await HistoryDetail({ live: l, kpi: l.kpi(mem.path)!, samples: [pt(1, 50), pt(2, 150)] })).toString();
    expect(html).toMatch(/^<article class="history/);
    expect(html).toContain('data-path="/iam/iam-api/metrics/go.memory.used.dat"');
    expect(html).toContain('id="chart"');
    expect(html).not.toContain("<html");
  });
  it("ships a hidden modal container on list pages so KPI links can open in place", async () => {
    const tab = (await TabularPage({ live: await live() })).toString();
    expect(tab).toMatch(/<div id="modal"[^>]*hidden/);
    const heat = (await HeatmapPage({ live: await live() }))!.toString();
    expect(heat).toMatch(/<div id="modal"[^>]*hidden/);
  });
});

describe("modal is hidden until opened", () => {
  it("has a hidden rule that beats its display:flex", async () => {
    const html = (await TabularPage({ live: await live() })).toString();
    expect(html).toContain("#modal[hidden] { display:none; }");
  });
});
