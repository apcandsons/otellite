// Server-rendered pages. The markup carries data-path / data-scope hooks
// that public/app.js uses to apply live updates in place.
import type { Child } from "hono/jsx";
import { scaleX, scaleY, sparklinePoints, ticks, timeline } from "./chart.js";
import { fmtAxis, fmtDuration, fmtRule, fmtTime, fmtValue, historyHref } from "./format.js";
import type { Live } from "./live.js";
import type { SamplePoint } from "./sor.js";
import { kpiWeight, rollup, type Kpi, type KpiSeverity, type Namespace, type Rollup, type Service } from "./status.js";
import { squarify, type Rect } from "./treemap.js";

const SPARK_W = 120;
const SPARK_H = 24;

const css = `
:root { color-scheme: light dark; --bg:#0f1217; --panel:#171b22; --line:#2a303a; --fg:#e6e8eb; --muted:#8b94a3;
  --red:#e5484d; --amber:#f5a524; --green:#30a46c; --unknown:#4b5563; }
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--fg); font:14px/1.45 system-ui, -apple-system, Segoe UI, sans-serif; }
header { display:flex; align-items:center; gap:18px; padding:10px 20px; border-bottom:1px solid var(--line); background:var(--panel); position:sticky; top:0; }
header h1 { font-size:15px; margin:0; letter-spacing:.02em; }
header nav a { color:var(--muted); text-decoration:none; margin-right:12px; }
header nav a.active, header nav a:hover { color:var(--fg); }
#conn { margin-left:auto; font-size:12px; color:var(--muted); }
#conn::before { content:""; display:inline-block; width:8px; height:8px; border-radius:50%; background:var(--unknown); margin-right:6px; }
#conn.live::before { background:var(--green); }
main { padding:16px 20px; max-width:1200px; margin:0 auto; }
a { color:inherit; }
.crumbs { color:var(--muted); margin-bottom:12px; }
.crumbs a { color:var(--muted); text-decoration:none; }
.crumbs a:hover { color:var(--fg); }
details { border:1px solid var(--line); border-radius:8px; background:var(--panel); margin-bottom:10px; }
details > summary { cursor:pointer; padding:10px 14px; display:flex; align-items:center; gap:10px; list-style:none; }
details > summary::-webkit-details-marker { display:none; }
details > summary::before { content:"▸"; color:var(--muted); width:12px; }
details[open] > summary::before { content:"▾"; }
details.ns > summary { font-weight:600; font-size:15px; }
details.svc { margin:0 14px 12px; }
.badges { display:inline-flex; gap:6px; margin-left:auto; }
.badge { display:inline-block; min-width:22px; padding:1px 7px; border-radius:999px; font-size:12px; text-align:center; color:#fff; }
.badge.red { background:var(--red); } .badge.amber { background:var(--amber); color:#1a1a1a; }
.badge[hidden] { display:none; }
table.kpis { width:100%; border-collapse:collapse; }
table.kpis td { padding:8px 14px; border-top:1px solid var(--line); vertical-align:middle; }
td.status { width:24px; } td.spark { width:140px; } td.value { font-variant-numeric:tabular-nums; white-space:nowrap; }
td.rules { color:var(--muted); font-size:12px; }
span.dot { display:inline-block; width:10px; height:10px; border-radius:50%; background:var(--unknown); }
.red span.dot { background:var(--red); } .amber span.dot { background:var(--amber); }
.green span.dot { background:var(--green); }
svg.spark polyline { fill:none; stroke:var(--muted); stroke-width:1.5; }
.red svg.spark polyline { stroke:var(--red); } .amber svg.spark polyline { stroke:var(--amber); } .green svg.spark polyline { stroke:var(--green); }
main.heat { max-width:none; padding:10px 12px 0; display:flex; flex-direction:column; height:calc(100vh - 45px); }
main.heat .crumbs { margin-bottom:8px; }
.treemap { position:relative; flex:1; min-height:420px; background:#000; }
.treemap section, .treemap .inner, .treemap .tile { position:absolute; box-sizing:border-box; }
.treemap section { background:#000; }
.treemap .inner { left:0; right:0; bottom:0; }
section.ns > .inner { top:0; }
section.svc > h3 { position:absolute; top:0; left:0; right:0; height:24px; margin:0; padding:0 8px; font-size:13px; line-height:24px; color:var(--fg); display:flex; align-items:center; gap:6px; white-space:nowrap; overflow:hidden; }
section.svc > .inner { top:24px; }
section h3 a { text-decoration:none; color:var(--fg); }
section h3 a.ns-link { color:var(--muted); } section h3 .sep { color:var(--muted); }
section h3 a:last-of-type::after { content:" ›"; color:var(--muted); }
.treemap .badges { margin-left:0; } .treemap .badge { font-size:10px; min-width:16px; padding:0 5px; line-height:16px; }
.tile { display:flex; flex-direction:column; align-items:center; justify-content:center; gap:1px; padding:4px; border:2px solid #000; text-decoration:none; color:#fff; text-align:center; overflow:hidden; }
.tile:hover { filter:brightness(1.15); z-index:1; }
.tile.red { background:#b93a3a; } .tile.amber { background:#c98a1a; } .tile.green { background:#2f8a55; } .tile.unknown { background:#3a3f47; }
.tile .kname { font-weight:600; font-size:13px; word-break:break-all; } .tile .kval { font-size:17px; font-variant-numeric:tabular-nums; } .tile .krule { font-size:11px; opacity:.85; }
.tile.sz-m .kname { font-size:11px; } .tile.sz-m .kval { font-size:13px; } .tile.sz-m .krule { display:none; }
.tile.sz-s .kname { font-size:10px; } .tile.sz-s .kval, .tile.sz-s .krule { display:none; }
.tile.sz-xs .kname, .tile.sz-xs .kval, .tile.sz-xs .krule { display:none; }
.legend { display:flex; align-items:center; justify-content:center; gap:18px; padding:8px 0 10px; font-size:12px; color:var(--muted); }
.legend span::before { content:""; display:inline-block; width:28px; height:12px; vertical-align:-2px; margin-right:6px; border-radius:2px; }
.legend .green::before { background:#2f8a55; } .legend .amber::before { background:#c98a1a; } .legend .red::before { background:#b93a3a; } .legend .unknown::before { background:#3a3f47; }
.history h2 { margin:0 0 4px; } .history .path { color:var(--muted); font-family:ui-monospace, monospace; font-size:12px; }
.history ul.rules { padding-left:18px; color:var(--muted); }
#chart { background:var(--panel); border:1px solid var(--line); border-radius:8px; padding:10px; margin-top:12px; }
#chart svg { width:100%; height:auto; display:block; }
svg.timeline path.series { fill:none; stroke:#7cb7ff; stroke-width:1.5; }
svg.timeline rect.gap { fill:var(--red); fill-opacity:.12; }
svg.timeline line.now { stroke:var(--muted); stroke-dasharray:2 3; }
svg.timeline line.threshold { stroke:var(--red); stroke-dasharray:4 3; }
svg.timeline .axis { stroke:var(--line); } svg.timeline text { fill:var(--muted); font-size:11px; }
.summary { color:var(--muted); font-size:12px; margin-top:6px; }
#modal { position:fixed; inset:0; z-index:10; display:flex; align-items:flex-start; justify-content:center; padding:40px 16px; overflow:auto; }
#modal[hidden] { display:none; }
#modal .backdrop { position:fixed; inset:0; background:rgba(0,0,0,.6); }
#modal .dialog { position:relative; width:min(1100px, 100%); background:var(--bg); border:1px solid var(--line); border-radius:10px; padding:18px 20px 20px; box-shadow:0 20px 60px rgba(0,0,0,.5); }
#modal .close { position:absolute; top:10px; right:12px; background:none; border:0; color:var(--muted); font-size:22px; line-height:1; cursor:pointer; }
#modal .close:hover { color:var(--fg); }
#modal .open-page { position:absolute; top:14px; right:44px; font-size:12px; color:var(--muted); }
#modal .body:empty::before { content:"loading…"; color:var(--muted); }
#modal .history h2 { padding-right:120px; }
.empty { color:var(--muted); padding:24px; text-align:center; }
form.login { max-width:360px; margin:60px auto; padding:24px; background:var(--panel); border:1px solid var(--line); border-radius:10px; display:flex; flex-direction:column; gap:12px; }
form.login h2 { margin:0 0 4px; font-size:16px; }
form.login label { display:flex; flex-direction:column; gap:6px; color:var(--muted); font-size:12px; }
form.login input[type=password] { font:inherit; padding:8px 10px; border:1px solid var(--line); border-radius:6px; background:var(--bg); color:var(--fg); }
form.login button { font:inherit; padding:8px 12px; border:0; border-radius:6px; background:#7cb7ff; color:#0f1217; cursor:pointer; }
form.login .error { margin:0; color:var(--red); font-size:13px; }
`;

function Layout(props: { title: string; active: "tabular" | "heatmap" | "history" | "login"; base: string; history?: string; mainClass?: string; bare?: boolean; children?: Child }) {
  const { base } = props;
  return (
    <html lang="en">
      <head>
        <meta charset="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <title>{props.title}</title>
        <style dangerouslySetInnerHTML={{ __html: css }} />
      </head>
      <body data-history={props.history} data-base={base}>
        <header>
          <h1>OTel Lite</h1>
          <nav>
            <a href={`${base}/`} class={props.active === "tabular" ? "active" : ""}>Tabular</a>
            <a href={`${base}/heatmap`} class={props.active === "heatmap" ? "active" : ""}>Heatmap</a>
          </nav>
          <span id="conn">connecting</span>
        </header>
        <main class={props.mainClass}>{props.children}</main>
        {!props.bare && (
          <div id="modal" hidden>
            <div class="backdrop"></div>
            <div class="dialog" role="dialog" aria-modal="true">
              <a class="open-page" href="#">open page ↗</a>
              <button class="close" aria-label="close" title="close (Esc)">×</button>
              <div class="body"></div>
            </div>
          </div>
        )}
        {!props.bare && <script src={`${base}/app.js`}></script>}
      </body>
    </html>
  );
}

function Empty(props: { live: Live }) {
  if (props.live.lastError) {
    return <p class="empty">Cannot reach the SoR: {props.live.lastError}. Is it running with -grpc (default :4319)? Retrying in the background.</p>;
  }
  return <p class="empty">No alert rules loaded. Start the SoR with -alerts.</p>;
}

function Badges(props: { scope: string; roll: Rollup }) {
  const { red, amber } = props.roll;
  return (
    <span class="badges" data-scope={props.scope} title="red: alerts firing · amber: over threshold">
      <span class="badge red" hidden={red === 0}>{red}</span>
      <span class="badge amber" hidden={amber === 0}>{amber}</span>
    </span>
  );
}

function Sparkline(props: { values: number[] }) {
  const values = props.values.filter((v) => Number.isFinite(v));
  return (
    <svg class="spark" viewBox={`0 0 ${SPARK_W} ${SPARK_H}`} width={SPARK_W} height={SPARK_H} data-values={JSON.stringify(values)}>
      <polyline points={sparklinePoints(values, SPARK_W, SPARK_H)} />
    </svg>
  );
}

const ruleText = (kpi: Kpi, unit?: string) => kpi.rules.map((r) => fmtRule(r, unit)).join(", ");

function statuses(live: Live, kpis: Kpi[]): KpiSeverity[] {
  return kpis.map((k) => live.status(k.path));
}
const svcRoll = (live: Live, s: Service) => rollup(statuses(live, s.kpis));
const nsRoll = (live: Live, n: Namespace) => rollup(statuses(live, n.services.flatMap((s) => s.kpis)));

function KpiRow(props: { live: Live; kpi: Kpi; base: string }) {
  const { live, kpi, base } = props;
  const latest = live.latest(kpi.path);
  return (
    <tr class={`kpi ${live.status(kpi.path)}`} data-path={kpi.path}>
      <td class="status"><span class="dot"></span></td>
      <td class="name"><a href={historyHref(kpi, base)}>{kpi.name}</a></td>
      <td class="value">{latest ? fmtValue(latest.raw, latest.unit) : "–"}</td>
      <td class="spark"><Sparkline values={live.recent(kpi.path).map((p) => p.value)} /></td>
      <td class="rules">{ruleText(kpi, latest?.unit)}</td>
    </tr>
  );
}

export function TabularPage(props: { live: Live; base?: string }) {
  const { live, base = "" } = props;
  return (
    <Layout title="OTel Lite" active="tabular" base={base}>
      {live.tree.length === 0 && <Empty live={live} />}
      {live.tree.map((ns) => (
        <details class="ns" data-ns={ns.name} open>
          <summary>
            <span class="name">{ns.name}</span>
            <Badges scope={`ns:${ns.name}`} roll={nsRoll(live, ns)} />
          </summary>
          {ns.services.map((svc) => (
            <details class="svc" data-svc={svc.name} data-ns={ns.name}>
              <summary>
                <span class="name">{svc.name}</span>
                <Badges scope={`svc:${ns.name}/${svc.name}`} roll={svcRoll(live, svc)} />
              </summary>
              <table class="kpis">
                <tbody>{svc.kpis.map((kpi) => <KpiRow live={live} kpi={kpi} base={base} />)}</tbody>
              </table>
            </details>
          ))}
        </details>
      ))}
    </Layout>
  );
}

// Approximate pixel size of the treemap, used only to pick label sizes.
const MAP_PX = { w: 1200, h: 720 };
const SVC_HEADER = 24;
const FULL: Rect = { x: 0, y: 0, w: 100, h: 100 };

const pct = (n: number) => `${Number(n.toFixed(3))}%`;
const place = (r: Rect) => `left:${pct(r.x)};top:${pct(r.y)};width:${pct(r.w)};height:${pct(r.h)}`;

function sizeClass(px: { w: number; h: number }): string {
  const m = Math.min(px.w, px.h);
  if (m < 30 || px.w < 60) return "sz-xs";
  if (m < 48) return "sz-s";
  if (m < 80 || px.w < 130) return "sz-m";
  return "";
}

function Tile(props: { live: Live; kpi: Kpi; rect: Rect; px: { w: number; h: number }; base: string }) {
  const { live, kpi, rect, px, base } = props;
  const latest = live.latest(kpi.path);
  const cls = ["tile", live.status(kpi.path), sizeClass(px)].filter(Boolean).join(" ");
  const value = latest ? fmtValue(latest.raw, latest.unit) : "–";
  const rules = ruleText(kpi, latest?.unit);
  return (
    <a class={cls} data-path={kpi.path} href={historyHref(kpi, base)} style={place(rect)} title={`${kpi.name}: ${value} (${rules})`}>
      <span class="kname">{kpi.name}</span>
      <span class="kval">{value}</span>
      <span class="krule">{rules}</span>
    </a>
  );
}

const weightOf = (live: Live, kpi: Kpi) => kpiWeight(kpi, live.latest(kpi.path)?.value);
const svcWeight = (live: Live, svc: Service) => svc.kpis.reduce((s, k) => s + weightOf(live, k), 0);
const nsWeight = (live: Live, ns: Namespace) => ns.services.reduce((s, svc) => s + svcWeight(live, svc), 0);

function ServiceSection(props: { live: Live; svc: Service; rect: Rect; px: { w: number; h: number }; base: string }) {
  const { live, svc, rect, px, base } = props;
  const inner = { w: px.w, h: Math.max(0, px.h - SVC_HEADER) };
  const tiles = squarify(svc.kpis.map((k) => ({ key: k.path, weight: weightOf(live, k) })), FULL);
  return (
    <section class="svc" data-svc={svc.name} style={place(rect)}>
      <h3>
        <a class="ns-link" href={`${base}/heatmap/${svc.namespace}`}>{svc.namespace}</a>
        <span class="sep">/</span>
        <a href={`${base}/heatmap/${svc.namespace}/${svc.name}`}>{svc.name}</a>
        <Badges scope={`svc:${svc.namespace}/${svc.name}`} roll={svcRoll(live, svc)} />
      </h3>
      <div class="inner">
        {tiles.map((t) => {
          const kpi = svc.kpis.find((k) => k.path === t.key)!;
          return <Tile live={live} kpi={kpi} rect={t} px={{ w: (inner.w * t.w) / 100, h: (inner.h * t.h) / 100 }} base={base} />;
        })}
      </div>
    </section>
  );
}

function NamespaceSection(props: { live: Live; ns: Namespace; rect: Rect; px: { w: number; h: number }; base: string }) {
  const { live, ns, rect, px, base } = props;
  const inner = { w: px.w, h: px.h };
  const cells = squarify(ns.services.map((s) => ({ key: s.name, weight: svcWeight(live, s) })), FULL);
  return (
    <section class="ns" data-ns={ns.name} style={place(rect)}>
      <div class="inner">
        {cells.map((c) => {
          const svc = ns.services.find((s) => s.name === c.key)!;
          return <ServiceSection live={live} svc={svc} rect={c} px={{ w: (inner.w * c.w) / 100, h: (inner.h * c.h) / 100 }} base={base} />;
        })}
      </div>
    </section>
  );
}

export function HeatmapPage(props: { live: Live; base?: string; namespace?: string; service?: string }) {
  const { live, namespace, service, base = "" } = props;
  let tree = live.tree;
  if (namespace !== undefined) {
    const ns = tree.find((n) => n.name === namespace);
    if (!ns) return null;
    if (service !== undefined) {
      const svc = ns.services.find((s) => s.name === service);
      if (!svc) return null;
      tree = [{ name: ns.name, services: [svc] }];
    } else {
      tree = [ns];
    }
  }
  const crumbs = [<a href={`${base}/heatmap`}>all</a>];
  if (namespace) crumbs.push(<span> / </span>, <a href={`${base}/heatmap/${namespace}`}>{namespace}</a>);
  if (namespace && service) crumbs.push(<span> / </span>, <a href={`${base}/heatmap/${namespace}/${service}`}>{service}</a>);
  const cells = squarify(tree.map((ns) => ({ key: ns.name, weight: nsWeight(live, ns) })), FULL);
  return (
    <Layout title="OTel Lite heatmap" active="heatmap" mainClass="heat" base={base}>
      <div class="crumbs">{crumbs}</div>
      {tree.length === 0 && <Empty live={live} />}
      {tree.length > 0 && (
        <div class="treemap">
          {cells.map((c) => {
            const ns = tree.find((n) => n.name === c.key)!;
            return <NamespaceSection live={live} ns={ns} rect={c} px={{ w: (MAP_PX.w * c.w) / 100, h: (MAP_PX.h * c.h) / 100 }} base={base} />;
          })}
        </div>
      )}
      <div class="legend">
        <span class="green">within threshold</span>
        <span class="amber">over threshold</span>
        <span class="red">alert firing (over threshold for its duration)</span>
        <span class="unknown">no data</span>
        <span>tile area grows with value ÷ threshold</span>
      </div>
    </Layout>
  );
}

const CHART_W = 960;
const CHART_H = 320;
const PAD = { left: 56, right: 40, top: 12, bottom: 28 };

/**
 * The chart fragment alone, so the client can refresh it in place. The x
 * axis always ends at `now`; stretches without samples are shaded.
 */
export function HistoryChart(props: { kpi: Kpi; samples: SamplePoint[]; now?: number }) {
  const { kpi, samples } = props;
  const now = props.now ?? Date.now();
  const w = CHART_W - PAD.left - PAD.right;
  const h = CHART_H - PAD.top - PAD.bottom;
  const thresholds = kpi.rules.filter((r) => r.op !== "absent").map((r) => r.threshold);
  const tl = timeline(samples, thresholds, { width: w, height: h }, { now });
  const silent = tl.gaps.reduce((s, g) => s + (g.to - g.from), 0);
  const yTicks = ticks(tl.yMin, tl.yMax, 4);
  const xTicks = ticks(tl.xMin, tl.xMax, 4);
  const unit = samples.at(-1)?.unit ?? "";
  if (samples.length === 0) return <p class="empty">No samples held for this stream.</p>;
  return (
    <>
      <svg class="timeline" viewBox={`0 0 ${CHART_W} ${CHART_H}`}>
        <g transform={`translate(${PAD.left},${PAD.top})`}>
          {yTicks.map((v) => (
            <g>
              <line class="axis" x1={0} x2={w} y1={scaleY(v, tl)} y2={scaleY(v, tl)} />
              <text x={-8} y={scaleY(v, tl) + 4} text-anchor="end">{fmtAxis(v, unit)}</text>
            </g>
          ))}
          {tl.gaps.map((g) => (
            <rect class="gap" x={scaleX(g.from, tl)} y={0} width={Math.max(1, scaleX(g.to, tl) - scaleX(g.from, tl))} height={h}>
              <title>no samples {fmtTime(g.from)} → {fmtTime(g.to)}</title>
            </rect>
          ))}
          <line class="now" x1={w} x2={w} y1={0} y2={h} />
          {xTicks.map((t, i) => (
            <text x={scaleX(t, tl)} y={h + 18} text-anchor={i === 0 ? "start" : i === xTicks.length - 1 ? "end" : "middle"}>
              {i === xTicks.length - 1 ? `now ${new Date(t).toLocaleTimeString("sv-SE")}` : new Date(t).toLocaleTimeString("sv-SE")}
            </text>
          ))}
          {kpi.rules.filter((r) => r.op !== "absent").map((r) => (
            <g>
              <line class="threshold" x1={0} x2={w} y1={scaleY(r.threshold, tl)} y2={scaleY(r.threshold, tl)} />
              <text x={w} y={scaleY(r.threshold, tl) - 4} text-anchor="end">{fmtRule(r, unit)}</text>
            </g>
          ))}
          <path class="series" d={tl.path} />
        </g>
      </svg>
      <div class="summary">
        {samples.length} samples{unit ? ` (${unit})` : ""}, {fmtTime(samples[0].time)} → {fmtTime(samples[samples.length - 1].time)}
        {tl.gaps.length > 0 && ` · ${tl.gaps.length} gap${tl.gaps.length > 1 ? "s" : ""} without samples (${fmtDuration(Math.round(silent / 1000))} shaded)`}
      </div>
    </>
  );
}

/** The KPI detail: heading, rules, chart. Served alone for the modal. */
export function HistoryDetail(props: { live: Live; kpi: Kpi; samples: SamplePoint[] }) {
  const { live, kpi, samples } = props;
  const latest = samples.at(-1);
  return (
    <article class={`history ${live.status(kpi.path)}`} data-path={kpi.path}>
      <h2><span class="dot"></span> {kpi.name} <span class="value">{latest ? fmtValue(latest.raw, latest.unit) : ""}</span></h2>
      <div class="path">{kpi.path}</div>
      <ul class="rules">
        {kpi.rules.map((r) => <li>{fmtRule(r, latest?.unit)} → {r.channel}</li>)}
      </ul>
      <div id="chart"><HistoryChart kpi={kpi} samples={samples} /></div>
    </article>
  );
}

export function HistoryPage(props: { live: Live; base?: string; kpi: Kpi; samples: SamplePoint[] }) {
  const { live, kpi, samples, base = "" } = props;
  return (
    <Layout title={`${kpi.name} history`} active="history" history={kpi.path} base={base}>
      <div class="crumbs">
        <a href={`${base}/`}>tabular</a> / <a href={`${base}/heatmap/${kpi.namespace}`}>{kpi.namespace}</a> / <a href={`${base}/heatmap/${kpi.namespace}/${kpi.service}`}>{kpi.service}</a>
      </div>
      <HistoryDetail live={live} kpi={kpi} samples={samples} />
    </Layout>
  );
}

/** The token form. Bare: no live script, since nothing behind it is reachable yet. */
export function LoginPage(props: { base: string; next: string; error?: boolean }) {
  return (
    <Layout title="OTel Lite login" active="login" base={props.base} bare>
      <form class="login" method="post" action={`${props.base}/login`}>
        <h2>Sign in</h2>
        {props.error && <p class="error">That token is not right.</p>}
        <input type="hidden" name="next" value={props.next} />
        <label>
          Token
          <input type="password" name="token" autocomplete="current-password" autofocus />
        </label>
        <button type="submit">Sign in</button>
      </form>
    </Layout>
  );
}

