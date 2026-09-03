// The web UI's view of the system of record. SorClient is the narrow
// interface the dashboard needs; connectSor implements it over gRPC.
import { createClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { timestampMs } from "@bufbuild/protobuf/wkt";
import { AlertState, SoRService, type Rule as PbRule, type Sample as PbSample } from "./gen/otellite/v1/sor_pb.js";
import type { Op, Rule } from "./status.js";

export interface SamplePoint {
  /** Unix milliseconds. */
  time: number;
  /** Parsed number, NaN when the sample is not numeric. */
  value: number;
  raw: string;
  unit: string;
}

export interface WatchEvent {
  path: string;
  sample: SamplePoint;
  alert?: { rule: Rule; state: "fired" | "resolved" };
}

export interface RuleState {
  rule: Rule;
  firing: boolean;
}

export interface SorClient {
  rules(): Promise<RuleState[]>;
  cat(path: string): Promise<SamplePoint[]>;
  watch(signal: AbortSignal): AsyncIterable<WatchEvent>;
}

export function connectSor(baseUrl: string): SorClient {
  const client = createClient(SoRService, createGrpcTransport({ baseUrl }));
  return {
    async rules() {
      const resp = await client.rules({});
      return resp.rules.map((rs) => ({ rule: toRule(rs.rule!), firing: rs.firing }));
    },
    async cat(path) {
      const resp = await client.cat({ path });
      return resp.samples.map(toPoint);
    },
    async *watch(signal) {
      for await (const ev of client.watch({}, { signal })) {
        const out: WatchEvent = { path: ev.path, sample: toPoint(ev.sample!) };
        if (ev.alert) {
          out.alert = {
            rule: toRule(ev.alert.rule!),
            state: ev.alert.state === AlertState.RESOLVED ? "resolved" : "fired",
          };
        }
        yield out;
      }
    },
  };
}

function toRule(r: PbRule): Rule {
  return {
    path: r.path,
    op: r.op as Op,
    threshold: r.threshold,
    holdForSeconds: Number(r.holdFor?.seconds ?? 0n),
    channel: r.channel,
  };
}

function toPoint(s: PbSample): SamplePoint {
  return { time: s.time ? timestampMs(s.time) : 0, value: Number(s.value), raw: s.value, unit: s.unit };
}
