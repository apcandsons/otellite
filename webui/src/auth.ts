// Token login for the dashboard. The operator's WEBUI_TOKEN is accepted as
// a bearer header (scripts, health probes) or exchanged once on /login for
// an HMAC session cookie, so the token itself never sits in the browser.
import { createHmac, timingSafeEqual } from "node:crypto";
import type { Context, MiddlewareHandler } from "hono";
import { getCookie } from "hono/cookie";

export const SESSION_COOKIE = "otellite_session";
const SESSION_MAX_AGE = 30 * 24 * 3600;

/** The cookie value for a token: a keyed hash, so a leaked cookie does not leak the token. */
export function sessionValue(token: string): string {
  return createHmac("sha256", token).update("otellite-session-v1").digest("hex");
}

export function safeEqual(a: string, b: string): boolean {
  const x = Buffer.from(a);
  const y = Buffer.from(b);
  return x.length === y.length && timingSafeEqual(x, y);
}

export function sessionCookie(token: string, base: string, secure: boolean): string {
  const parts = [`${SESSION_COOKIE}=${sessionValue(token)}`, "HttpOnly", "SameSite=Lax", `Path=${base || "/"}`, `Max-Age=${SESSION_MAX_AGE}`];
  if (secure) parts.push("Secure");
  return parts.join("; ");
}

/** Where to go after login: only paths under the base, never another host. */
export function safeNext(next: string | undefined, base: string): string {
  const home = `${base}/`;
  if (!next || !next.startsWith(home) || next.includes("//") || next.includes("\\") || /[\r\n]/.test(next)) return home;
  return next;
}

export function isAuthenticated(c: Context, token: string): boolean {
  const auth = c.req.header("authorization") ?? "";
  if (auth.startsWith("Bearer ") && safeEqual(auth.slice("Bearer ".length).trim(), token)) return true;
  const cookie = getCookie(c, SESSION_COOKIE);
  return cookie !== undefined && safeEqual(cookie, sessionValue(token));
}

const wantsHTML = (c: Context) => c.req.method === "GET" && (c.req.header("accept") ?? "").includes("text/html");

/**
 * Gate every route except `open`. Browsers navigating to a page are sent
 * to the login form with the page as `next`; anything else gets a 401.
 */
export function requireAuth(o: { token: string; base: string; open: string[] }): MiddlewareHandler {
  const open = new Set(o.open);
  return async (c, next) => {
    if (open.has(c.req.path.replace(/\/+$/, "")) || isAuthenticated(c, o.token)) return next();
    if (wantsHTML(c)) {
      const u = new URL(c.req.url);
      return c.redirect(`${o.base}/login?next=${encodeURIComponent(u.pathname + u.search)}`, 302);
    }
    return c.text("unauthorized", 401);
  };
}
