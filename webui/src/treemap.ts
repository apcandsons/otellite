// Squarified treemap layout (Bruls, Huizing, van Wijk). Pure geometry:
// items keep their input order within each row and get area in proportion
// to weight.

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface Item {
  key: string;
  weight: number;
}

export interface Placed extends Rect {
  key: string;
}

const MIN_WEIGHT_RATIO = 0.02; // a zero-weight item still gets a sliver

export function squarify(items: Item[], box: Rect): Placed[] {
  if (items.length === 0) return [];
  const maxW = Math.max(...items.map((i) => i.weight), 0);
  const floor = maxW > 0 ? maxW * MIN_WEIGHT_RATIO : 1;
  const weights = items.map((i) => (i.weight > 0 ? i.weight : floor));
  const total = weights.reduce((s, w) => s + w, 0);
  const scale = (box.w * box.h) / total; // area per unit weight

  const out: Placed[] = [];
  let free: Rect = { ...box };
  let row: number[] = []; // indexes into items
  let i = 0;
  while (i < items.length) {
    const side = Math.min(free.w, free.h);
    const next = [...row, i];
    if (row.length === 0 || worst(row, side, weights, scale) >= worst(next, side, weights, scale)) {
      row = next;
      i++;
    } else {
      free = layoutRow(row, free, weights, scale, items, out);
      row = [];
    }
  }
  if (row.length) layoutRow(row, free, weights, scale, items, out);
  return out;
}

/** Highest aspect ratio a row would have when laid along `side`. */
function worst(row: number[], side: number, weights: number[], scale: number): number {
  const areas = row.map((i) => weights[i] * scale);
  const sum = areas.reduce((s, a) => s + a, 0);
  const max = Math.max(...areas);
  const min = Math.min(...areas);
  const s2 = side * side;
  return Math.max((s2 * max) / (sum * sum), (sum * sum) / (s2 * min));
}

/** Places one row along the shorter side of `free` and returns what is left. */
function layoutRow(row: number[], free: Rect, weights: number[], scale: number, items: Item[], out: Placed[]): Rect {
  const sum = row.reduce((s, i) => s + weights[i] * scale, 0);
  if (free.w >= free.h) {
    // Row is a vertical strip on the left.
    const stripW = free.h > 0 ? sum / free.h : 0;
    let y = free.y;
    for (const i of row) {
      const h = stripW > 0 ? (weights[i] * scale) / stripW : 0;
      out.push({ key: items[i].key, x: free.x, y, w: stripW, h });
      y += h;
    }
    return { x: free.x + stripW, y: free.y, w: free.w - stripW, h: free.h };
  }
  // Row is a horizontal strip along the top.
  const stripH = free.w > 0 ? sum / free.w : 0;
  let x = free.x;
  for (const i of row) {
    const w = stripH > 0 ? (weights[i] * scale) / stripH : 0;
    out.push({ key: items[i].key, x, y: free.y, w, h: stripH });
    x += w;
  }
  return { x: free.x, y: free.y + stripH, w: free.w, h: free.h - stripH };
}
