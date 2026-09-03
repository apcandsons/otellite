// otellite-webui: a Hono server that renders the alert dashboard from the
// SoR's gRPC API and relays its live feed to browsers over SSE.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { serve } from "@hono/node-server";
import { createApp, normalizeBase } from "./app.js";
import { Live } from "./live.js";
import { connectSor } from "./sor.js";

const { values: args } = parseArgs({
  options: {
    sor: { type: "string", default: process.env.SOR_GRPC ?? "http://localhost:4319" },
    port: { type: "string", default: process.env.PORT ?? "8080" },
    base: { type: "string", default: process.env.BASE_PATH ?? "" },
    token: { type: "string", default: process.env.WEBUI_TOKEN ?? "" },
  },
});
const port = Number(args.port);
const base = normalizeBase(args.base);
const token = args.token ?? "";
const cookieSecure = process.env.COOKIE_SECURE !== "false";
const appJs = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", "public", "app.js"), "utf8");

const sor = connectSor(args.sor!);
const live = new Live(sor, { log: (m) => console.log(`webui: ${m}`) });
const app = createApp({ live, sor, appJs, basePath: base, token, cookieSecure });

if (!token) {
  console.warn("webui: WARNING: WEBUI_TOKEN is not set; the dashboard is open to anyone who can reach this port");
}

await live.load().catch((err) => console.log(`webui: initial load failed, retrying in background: ${err.message ?? err}`));
void live.run();

serve({ fetch: app.fetch, port }, () => {
  console.log(`webui: http://localhost:${port}${base}/ (SoR gRPC at ${args.sor}${token ? ", login required" : ""})`);
});
