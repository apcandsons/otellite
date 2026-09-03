import { describe, expect, it } from "vitest";
import { squarify, type Rect } from "../src/treemap.js";

const box: Rect = { x: 0, y: 0, w: 100, h: 60 };
const area = (r: Rect) => r.w * r.h;

describe("squarify", () => {
  it("fills the box with one item", () => {
    expect(squarify([{ key: "a", weight: 5 }], box)).toEqual([{ key: "a", x: 0, y: 0, w: 100, h: 60 }]);
  });

  it("gives each item area proportional to its weight and stays inside the box", () => {
    const items = [{ key: "a", weight: 6 }, { key: "b", weight: 3 }, { key: "c", weight: 2 }, { key: "d", weight: 1 }];
    const rects = squarify(items, box);
    expect(rects.map((r) => r.key)).toEqual(["a", "b", "c", "d"]);
    const total = area(box);
    for (const r of rects) {
      const item = items.find((i) => i.key === r.key)!;
      expect(area(r)).toBeCloseTo((item.weight / 12) * total, 6);
      expect(r.x).toBeGreaterThanOrEqual(0);
      expect(r.y).toBeGreaterThanOrEqual(0);
      expect(r.x + r.w).toBeLessThanOrEqual(100 + 1e-9);
      expect(r.y + r.h).toBeLessThanOrEqual(60 + 1e-9);
    }
    expect(rects.reduce((s, r) => s + area(r), 0)).toBeCloseTo(total, 6);
  });

  it("does not overlap", () => {
    const rects = squarify([1, 1, 1, 1, 1, 1, 1].map((w, i) => ({ key: String(i), weight: w })), box);
    for (const a of rects) for (const b of rects) {
      if (a === b) continue;
      const overlap = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x) > 1e-9 && Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y) > 1e-9;
      expect(overlap).toBe(false);
    }
  });

  it("prefers square-ish tiles over thin strips", () => {
    const rects = squarify([1, 1, 1, 1].map((w, i) => ({ key: String(i), weight: w })), { x: 0, y: 0, w: 100, h: 100 });
    for (const r of rects) expect(Math.max(r.w / r.h, r.h / r.w)).toBeLessThan(1.5);
  });

  it("treats non-positive weights as tiny but present, and empty input as empty", () => {
    expect(squarify([], box)).toEqual([]);
    const rects = squarify([{ key: "a", weight: 0 }, { key: "b", weight: 10 }], box);
    expect(rects).toHaveLength(2);
    expect(area(rects[0])).toBeGreaterThan(0);
  });
});
