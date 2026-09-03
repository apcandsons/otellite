import { describe, expect, it } from "vitest";
import { createApp } from "../src/app.js";
import { sessionValue } from "../src/auth.js";
import { Live } from "../src/live.js";
import type { RuleState, SamplePoint, SorClient } from "../src/sor.js";
import type { Rule } from "../src/status.js";

const mem: Rule = { path: "/iam/iam-api/metrics/go.memory.used.dat", op: ">", threshold: 100, holdForSeconds: 60, channel: "ops" };
const pt = (time: number, value: number, unit = "By"): SamplePoint => ({ time, value, raw: String(value), unit });

function sor(rules: RuleState[], history: Record<string, SamplePoint[]>): SorClient {
  return { rules: async () => rules, cat: async (p) => history[p] ?? [], async *watch() {} };
}

async function make(o: { basePath?: string; token?: string; cookieSecure?: boolean } = {}) {
  const s = sor([{ rule: mem, firing: false }], { [mem.path]: [pt(1, 50), pt(2, 150)] });
  const live = new Live(s);
  await live.load();
  return createApp({ live, sor: s, appJs: "// app", ...o });
}

const html = { accept: "text/html,application/xhtml+xml" };

describe("routes without a base path", () => {
  it("serves pages, fragments, app.js and healthz", async () => {
    const app = await make();
    expect((await app.request("/")).status).toBe(200);
    expect(await (await app.request("/")).text()).toContain('data-ns="iam"');
    expect((await app.request("/heatmap")).status).toBe(200);
    expect((await app.request("/heatmap/iam")).status).toBe(200);
    expect((await app.request("/heatmap/iam/iam-api")).status).toBe(200);
    expect((await app.request("/heatmap/nope")).status).toBe(404);
    expect((await app.request("/history/iam/iam-api/go.memory.used")).status).toBe(200);
    expect((await app.request("/history/iam/iam-api/nope")).status).toBe(404);

    const detail = await app.request("/_/detail/iam/iam-api/go.memory.used");
    expect(detail.status).toBe(200);
    expect(await detail.text()).toMatch(/^<article class="history/);
    const chart = await app.request("/_/history/iam/iam-api/go.memory.used");
    expect(chart.status).toBe(200);
    expect(await chart.text()).toContain("<svg");
    expect((await app.request("/_/detail/iam/iam-api/nope")).status).toBe(404);

    const js = await app.request("/app.js");
    expect(js.status).toBe(200);
    expect(js.headers.get("content-type")).toContain("text/javascript");
    expect(await js.text()).toBe("// app");

    const hz = await app.request("/healthz");
    expect(hz.status).toBe(200);
    expect(await hz.text()).toBe("ok");
  });
  it("no longer serves the old /api/* fragment routes", async () => {
    const app = await make();
    expect((await app.request("/api/detail/iam/iam-api/go.memory.used")).status).toBe(404);
    expect((await app.request("/api/history/iam/iam-api/go.memory.used")).status).toBe(404);
  });
  it("renders root-relative links and an empty data-base", async () => {
    const app = await make();
    const body = await (await app.request("/")).text();
    expect(body).toContain('data-base=""');
    expect(body).toContain('href="/history/iam/iam-api/go.memory.used"');
    expect(body).toContain('src="/app.js"');
    expect(body).toContain('href="/heatmap"');
  });
});

describe("with a base path", () => {
  it("serves everything under the base and nothing at the root", async () => {
    const app = await make({ basePath: "/otellite" });
    expect((await app.request("/otellite/")).status).toBe(200);
    expect((await app.request("/otellite")).status).toBe(200);
    expect((await app.request("/otellite/heatmap/iam/iam-api")).status).toBe(200);
    expect((await app.request("/otellite/history/iam/iam-api/go.memory.used")).status).toBe(200);
    expect((await app.request("/otellite/_/detail/iam/iam-api/go.memory.used")).status).toBe(200);
    expect((await app.request("/otellite/_/history/iam/iam-api/go.memory.used")).status).toBe(200);
    expect((await app.request("/otellite/app.js")).status).toBe(200);
    expect((await app.request("/otellite/healthz")).status).toBe(200);
    expect((await app.request("/")).status).toBe(404);
    expect((await app.request("/heatmap")).status).toBe(404);
    expect((await app.request("/app.js")).status).toBe(404);
    expect((await app.request("/otellite/api/detail/iam/iam-api/go.memory.used")).status).toBe(404);
  });
  it("prefixes every link, the script and data-base", async () => {
    const app = await make({ basePath: "/otellite" });
    const tab = await (await app.request("/otellite/")).text();
    expect(tab).toContain('data-base="/otellite"');
    expect(tab).toContain('href="/otellite/history/iam/iam-api/go.memory.used"');
    expect(tab).toContain('src="/otellite/app.js"');
    expect(tab).toContain('href="/otellite/heatmap"');
    expect(tab).toContain('href="/otellite/"');
    const heat = await (await app.request("/otellite/heatmap")).text();
    expect(heat).toContain('href="/otellite/heatmap/iam"');
    expect(heat).toContain('href="/otellite/heatmap/iam/iam-api"');
    expect(heat).toContain('href="/otellite/history/iam/iam-api/go.memory.used"');
    const hist = await (await app.request("/otellite/history/iam/iam-api/go.memory.used")).text();
    expect(hist).toContain('href="/otellite/"');
    expect(hist).toContain('href="/otellite/heatmap/iam"');
    expect(hist).not.toMatch(/href="\/(heatmap|history|app\.js)/);
  });
  it("accepts a base path with a trailing slash", async () => {
    const app = await make({ basePath: "/otellite/" });
    expect((await app.request("/otellite/heatmap")).status).toBe(200);
    expect(await (await app.request("/otellite/")).text()).toContain('data-base="/otellite"');
  });
});

describe("auth off", () => {
  it("serves pages without credentials and sends /login home", async () => {
    const app = await make();
    expect((await app.request("/", { headers: html })).status).toBe(200);
    const login = await app.request("/login");
    expect(login.status).toBe(302);
    expect(login.headers.get("location")).toBe("/");
  });
});

describe("auth on", () => {
  const token = "s3cret";
  const cookie = `otellite_session=${sessionValue(token)}`;

  it("redirects HTML navigation to the login page with next", async () => {
    const app = await make({ basePath: "/otellite", token });
    const r = await app.request("/otellite/heatmap/iam?x=1", { headers: html });
    expect(r.status).toBe(302);
    expect(r.headers.get("location")).toBe("/otellite/login?next=%2Fotellite%2Fheatmap%2Fiam%3Fx%3D1");
  });
  it("answers 401 to everything else without credentials", async () => {
    const app = await make({ basePath: "/otellite", token });
    expect((await app.request("/otellite/")).status).toBe(401);
    expect((await app.request("/otellite/_/detail/iam/iam-api/go.memory.used")).status).toBe(401);
    expect((await app.request("/otellite/events")).status).toBe(401);
    expect((await app.request("/otellite/", { method: "POST", headers: html })).status).toBe(401);
  });
  it("accepts a bearer token or the session cookie", async () => {
    const app = await make({ basePath: "/otellite", token });
    expect((await app.request("/otellite/", { headers: { authorization: `Bearer ${token}` } })).status).toBe(200);
    expect((await app.request("/otellite/", { headers: { authorization: "Bearer nope" } })).status).toBe(401);
    expect((await app.request("/otellite/", { headers: { cookie } })).status).toBe(200);
    expect((await app.request("/otellite/", { headers: { cookie: "otellite_session=deadbeef" } })).status).toBe(401);
    expect((await app.request("/otellite/_/detail/iam/iam-api/go.memory.used", { headers: { cookie } })).status).toBe(200);
  });
  it("leaves login, healthz and app.js open", async () => {
    const app = await make({ basePath: "/otellite", token });
    expect((await app.request("/otellite/healthz")).status).toBe(200);
    expect((await app.request("/otellite/app.js")).status).toBe(200);
    const login = await app.request("/otellite/login?next=%2Fotellite%2Fheatmap");
    expect(login.status).toBe(200);
    const form = await login.text();
    expect(form).toContain('action="/otellite/login"');
    expect(form).toContain('name="next" value="/otellite/heatmap"');
    expect(form).toContain('type="password"');
  });
  it("sets the session cookie on a correct token and redirects to next", async () => {
    const app = await make({ basePath: "/otellite", token });
    const r = await app.request("/otellite/login", {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ token, next: "/otellite/heatmap" }).toString(),
    });
    expect(r.status).toBe(302);
    expect(r.headers.get("location")).toBe("/otellite/heatmap");
    const set = r.headers.get("set-cookie")!;
    expect(set).toContain(`otellite_session=${sessionValue(token)}`);
    expect(set).toContain("HttpOnly");
    expect(set).toContain("SameSite=Lax");
    expect(set).toContain("Path=/otellite");
    expect(set).toContain("Secure");
    expect(set).toContain("Max-Age=2592000");
  });
  it("omits Secure when cookieSecure is false", async () => {
    const app = await make({ token, cookieSecure: false });
    const r = await app.request("/login", {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ token }).toString(),
    });
    expect(r.status).toBe(302);
    expect(r.headers.get("location")).toBe("/");
    const set = r.headers.get("set-cookie")!;
    expect(set).not.toContain("Secure");
    expect(set).toContain("Path=/;");
  });
  it("rejects a wrong token with the form again", async () => {
    const app = await make({ basePath: "/otellite", token });
    const r = await app.request("/otellite/login", {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ token: "nope" }).toString(),
    });
    expect(r.status).toBe(401);
    expect(r.headers.get("set-cookie")).toBeNull();
    expect(await r.text()).toContain('action="/otellite/login"');
  });
  it("only follows next inside the base path", async () => {
    const app = await make({ basePath: "/otellite", token });
    for (const next of ["https://evil.com", "//evil.com", "/elsewhere", "/otellite//evil.com", "/otellite\\evil.com", "otellite/x", ""]) {
      const r = await app.request("/otellite/login", {
        method: "POST",
        headers: { "content-type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({ token, next }).toString(),
      });
      expect(r.status, next).toBe(302);
      expect(r.headers.get("location"), next).toBe("/otellite/");
    }
  });
  it("redirects to /login at the root when there is no base path", async () => {
    const app = await make({ token });
    const r = await app.request("/", { headers: html });
    expect(r.status).toBe(302);
    expect(r.headers.get("location")).toBe("/login?next=%2F");
  });
});
