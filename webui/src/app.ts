// The Hono app, separated from main.ts so tests can drive it with
// app.request() against a fake SoR.
import { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import { requireAuth, safeEqual, safeNext, sessionCookie } from "./auth.js";
import type { Live } from "./live.js";
import type { SorClient } from "./sor.js";
import { HeatmapPage, HistoryChart, HistoryDetail, HistoryPage, LoginPage, TabularPage } from "./views.js";

export interface AppOptions {
  live: Live;
  sor: SorClient;
  appJs: string;
  /** Mount point when served behind a reverse proxy, e.g. "/otellite". */
  basePath?: string;
  /** Login token; empty disables authentication. */
  token?: string;
  /** Mark the session cookie Secure (default true; false for plain-http dev). */
  cookieSecure?: boolean;
}

/** "" for the root, otherwise "/prefix" with no trailing slash. */
export function normalizeBase(p?: string): string {
  let b = (p ?? "").trim();
  if (!b.startsWith("/")) b = "/" + b;
  b = b.replace(/\/+$/, "");
  return b === "" ? "" : b;
}

export function createApp(o: AppOptions) {
  const { live, sor } = o;
  const base = normalizeBase(o.basePath);
  const token = o.token ?? "";
  const cookieSecure = o.cookieSecure ?? true;
  const home = `${base}/`;

  const app = new Hono({ strict: false }).basePath(base);

  if (token) {
    app.use("*", requireAuth({ token, base, open: [`${base}/login`, `${base}/healthz`, `${base}/app.js`] }));
  }

  app.get("/healthz", (c) => c.text("ok"));
  app.get("/app.js", (c) => c.body(o.appJs, 200, { "Content-Type": "text/javascript; charset=utf-8" }));

  app.get("/login", (c) => {
    if (!token) return c.redirect(home, 302);
    return c.html(LoginPage({ base, next: safeNext(c.req.query("next"), base) }));
  });
  app.post("/login", async (c) => {
    if (!token) return c.redirect(home, 302);
    const body = await c.req.parseBody();
    const given = typeof body.token === "string" ? body.token : "";
    const next = safeNext(typeof body.next === "string" ? body.next : undefined, base);
    if (!safeEqual(given, token)) return c.html(LoginPage({ base, next, error: true }), 401);
    c.header("Set-Cookie", sessionCookie(token, base, cookieSecure));
    return c.redirect(next, 302);
  });

  app.get("/", (c) => c.html(TabularPage({ live, base })));

  app.get("/heatmap", (c) => c.html(HeatmapPage({ live, base })!));
  app.get("/heatmap/:ns", (c) => {
    const page = HeatmapPage({ live, base, namespace: c.req.param("ns") });
    return page ? c.html(page) : c.notFound();
  });
  app.get("/heatmap/:ns/:svc", (c) => {
    const page = HeatmapPage({ live, base, namespace: c.req.param("ns"), service: c.req.param("svc") });
    return page ? c.html(page) : c.notFound();
  });

  const streamPath = (c: { req: { param(k: string): string } }) =>
    `/${c.req.param("ns")}/${c.req.param("svc")}/metrics/${c.req.param("name")}.dat`;

  app.get("/history/:ns/:svc/:name", async (c) => {
    const kpi = live.kpi(streamPath(c));
    if (!kpi) return c.notFound();
    const samples = await sor.cat(kpi.path).catch(() => []);
    return c.html(HistoryPage({ live, base, kpi, samples }));
  });

  // Fragments live under /_/ so a reverse proxy can treat /api/ as
  // something else. Detail loads into the modal when a KPI is clicked;
  // the chart is refetched by the client when new samples arrive.
  app.get("/_/detail/:ns/:svc/:name", async (c) => {
    const kpi = live.kpi(streamPath(c));
    if (!kpi) return c.notFound();
    const samples = await sor.cat(kpi.path).catch(() => []);
    return c.html(HistoryDetail({ live, kpi, samples }));
  });
  app.get("/_/history/:ns/:svc/:name", async (c) => {
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

  return app;
}
