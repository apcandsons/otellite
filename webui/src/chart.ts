// SVG geometry for sparklines and the history timeline. Pure functions.

export interface Point {
  time: number;
  value: number;
}

const fmt = (n: number): string => Number(n.toFixed(1)).toString();

/** Polyline points for a sparkline box, oldest on the left. */
export function sparklinePoints(values: number[], width: number, height: number): string {
  const vs = values.filter((v) => Number.isFinite(v));
  if (vs.length < 2) return "";
  const min = Math.min(...vs);
  const max = Math.max(...vs);
  const span = max - min;
  return vs
    .map((v, i) => {
      const x = (i / (vs.length - 1)) * width;
      const y = span === 0 ? height / 2 : height - ((v - min) / span) * height;
      return `${fmt(x)},${fmt(y)}`;
    })
    .join(" ");
}

export interface Gap {
  from: number;
  to: number;
}

export interface Timeline {
  width: number;
  height: number;
  xMin: number;
  xMax: number;
  yMin: number;
  yMax: number;
  /** SVG path of the series, one M…L run per stretch of contiguous samples. */
  path: string;
  /** Stretches with no samples, including a trailing one up to now. */
  gaps: Gap[];
}

export interface TimelineOptions {
  /** Right edge of the x axis. Defaults to the last sample. */
  now?: number;
  /** Spacing beyond which samples are considered disconnected. Defaults to 3x the median spacing. */
  gap?: number;
}

/** Spacing beyond which two samples no longer count as contiguous. */
export function gapThreshold(times: number[], explicit?: number): number {
  if (explicit !== undefined) return explicit;
  const deltas = times.slice(1).map((t, i) => t - times[i]).filter((d) => d > 0).sort((a, b) => a - b);
  if (deltas.length === 0) return Infinity;
  return 3 * deltas[Math.floor(deltas.length / 2)];
}

export function scaleX(t: number, tl: Timeline): number {
  const span = tl.xMax - tl.xMin;
  return span === 0 ? 0 : ((t - tl.xMin) / span) * tl.width;
}

export function scaleY(v: number, tl: Timeline): number {
  const span = tl.yMax - tl.yMin;
  return span === 0 ? tl.height / 2 : tl.height - ((v - tl.yMin) / span) * tl.height;
}

/** Scales a series (and any threshold lines) into a width x height box. */
export function timeline(points: Point[], thresholds: number[], box: { width: number; height: number }, opts: TimelineOptions = {}): Timeline {
  const pts = points.filter((p) => Number.isFinite(p.value));
  const ys = [...pts.map((p) => p.value), ...thresholds];
  let yMin = ys.length ? Math.min(...ys) : 0;
  let yMax = ys.length ? Math.max(...ys) : 1;
  if (yMin === yMax) {
    const pad = Math.abs(yMin) * 0.1 || 1;
    yMin -= pad;
    yMax += pad;
  }
  const last = pts.length ? pts[pts.length - 1].time : 1;
  const tl: Timeline = {
    ...box,
    xMin: pts.length ? pts[0].time : 0,
    xMax: opts.now !== undefined ? Math.max(opts.now, last) : last,
    yMin,
    yMax,
    path: "",
    gaps: [],
  };
  const gap = gapThreshold(pts.map((p) => p.time), opts.gap);
  const parts: string[] = [];
  pts.forEach((p, i) => {
    const broken = i === 0 || p.time - pts[i - 1].time > gap;
    if (i > 0 && broken) tl.gaps.push({ from: pts[i - 1].time, to: p.time });
    parts.push(`${broken ? "M" : "L"}${fmt(scaleX(p.time, tl))},${fmt(scaleY(p.value, tl))}`);
  });
  if (pts.length && tl.xMax - last > gap) tl.gaps.push({ from: last, to: tl.xMax });
  tl.path = parts.join(" ");
  return tl;
}

/** n+1 evenly spaced values from min to max inclusive. */
export function ticks(min: number, max: number, n: number): number[] {
  return Array.from({ length: n + 1 }, (_, i) => min + ((max - min) * i) / n);
}
