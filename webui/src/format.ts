import type { Rule } from "./status.js";

export const isBytes = (unit?: string): boolean => unit === "By" || unit === "Bytes";

/** Byte count as B / KB / MB / GB / TB, base 1024, one decimal. */
export function fmtBytes(n: number): string {
  if (Math.abs(n) < 1024) return `${Math.round(n)} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (i < units.length - 1 && Math.abs(v) >= 1024) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}

export function fmtValue(raw: string, unit: string): string {
  const v = Number(raw);
  if (isBytes(unit) && Number.isFinite(v)) return fmtBytes(v);
  let text = raw;
  if (Number.isFinite(v) && !Number.isInteger(v)) {
    // 4 significant digits for small numbers, whole numbers beyond that; never exponent notation.
    text = Math.abs(v) >= 1000 ? String(Math.round(v)) : String(Number(v.toPrecision(4)));
  }
  return unit && unit !== "1" ? `${text} ${unit}` : text; // "1" is OTel's dimensionless unit
}

export function fmtDuration(seconds: number): string {
  if (seconds % 3600 === 0 && seconds > 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0 && seconds > 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}

export function fmtRule(r: Rule, unit?: string): string {
  if (r.op === "absent") return `absent for ${fmtDuration(r.holdForSeconds)}`;
  const threshold = isBytes(unit) ? fmtBytes(r.threshold) : String(r.threshold);
  return `${r.op} ${threshold} for ${fmtDuration(r.holdForSeconds)}`;
}

export function fmtTime(ms: number): string {
  return new Date(ms).toLocaleString("sv-SE", { timeZoneName: "short" });
}

/** History page location for a KPI. */
export function historyHref(k: { namespace: string; service: string; name: string }): string {
  return `/history/${k.namespace}/${k.service}/${k.name}`;
}

/** Compact axis label: 4 significant digits with k / M / G suffixes. */
export function fmtAxis(v: number, unit?: string): string {
  if (isBytes(unit)) return fmtBytes(v);
  if (v === 0) return "0";
  const abs = Math.abs(v);
  const units: [number, string][] = [[1e9, "G"], [1e6, "M"], [1e3, "k"]];
  for (const [scale, suffix] of units) {
    if (abs >= scale) return `${Number((v / scale).toPrecision(4))}${suffix}`;
  }
  return String(Number(v.toPrecision(4)));
}
