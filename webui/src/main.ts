// otellite-webui: a Hono server that renders the alert dashboard from the
// SoR's gRPC API and relays its live feed to browsers over SSE.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { serve } from "@hono/node-server";
import { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import { Live } from "./live.js";
import { connectSor } from "./sor.js";
import { HeatmapPage, HistoryChart, HistoryDetail, HistoryPage, TabularPage } from "./views.js";

const { values: args } = parseArgs({
  options: {
    sor: { type: "string", default: process.env.SOR_GRPC ?? "http://localhost:4319" },
    port: { type: "string", default: process.env.PORT ?? "8080" },
  },
});
const port = Number(args.port);
const appJs = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", "public", "app.js"), "utf8");

const sor = connectSor(args.sor!);
const live = new Live(sor, { log: (m) => console.log(`webui: ${m}`) });

const app = new Hono();

app.get("/app.js", (c) => c.body(appJs, 200, { "Content-Type": "text/javascript; charset=utf-8" }));

app.get("/", (c) => c.html(TabularPage({ live })));

app.get("/heatmap", (c) => c.html(HeatmapPage({ live })!));
app.get("/heatmap/:ns", (c) => {
  const page = HeatmapPage({ live, namespace: c.req.param("ns") });
  return page ? c.html(page) : c.notFound();
});
app.get("/heatmap/:ns/:svc", (c) => {
  const page = HeatmapPage({ live, namespace: c.req.param("ns"), service: c.req.param("svc") });
  return page ? c.html(page) : c.notFound();
});

const streamPath = (c: { req: { param(k: string): string } }) =>
  `/${c.req.param("ns")}/${c.req.param("svc")}/metrics/${c.req.param("name")}.dat`;

app.get("/history/:ns/:svc/:name", async (c) => {
  const kpi = live.kpi(streamPath(c));
  if (!kpi) return c.notFound();
  const samples = await sor.cat(kpi.path).catch(() => []);
  return c.html(HistoryPage({ live, kpi, samples }));
});

// Detail fragment, loaded into the modal when a KPI is clicked.
app.get("/api/detail/:ns/:svc/:name", async (c) => {
  const kpi = live.kpi(streamPath(c));
  if (!kpi) return c.notFound();
  const samples = await sor.cat(kpi.path).catch(() => []);
  return c.html(HistoryDetail({ live, kpi, samples }));
});

// Chart fragment, refetched by the client when new samples arrive.
app.get("/api/history/:ns/:svc/:name", async (c) => {
  const kpi = live.kpi(streamPath(c));
  if (!kpi) return c.notFound();
  const samples = await sor.cat(kpi.path).catch(() => []);
  return c.html(HistoryChart({ kpi, samples }));
});

app.get("/events", (c) =>
  streamSSE(c, async (stream) => {
    let open = true;
    const off = live.onUpdate((u) => {
      if (open) void stream.writeSSE({ event: "update", data: JSON.stringify(u) });
    });
    stream.onAbort(() => {
      open = false;
      off();
    });
    while (open) {
      await stream.writeSSE({ event: "ping", data: String(Date.now()) });
      await stream.sleep(15000);
    }
  }),
);

await live.load().catch((err) => console.log(`webui: initial load failed, retrying in background: ${err.message ?? err}`));
void live.run();

serve({ fetch: app.fetch, port }, () => {
  console.log(`webui: http://localhost:${port} (SoR gRPC at ${args.sor})`);
});
