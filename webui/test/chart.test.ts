import { describe, expect, it } from "vitest";
import { scaleY, sparklinePoints, ticks, timeline } from "../src/chart.js";

describe("sparklinePoints", () => {
  it("maps values across the box, newest on the right", () => {
    expect(sparklinePoints([0, 5, 10], 100, 20)).toBe("0,20 50,10 100,0");
  });
  it("draws a flat line for constant values", () => {
    expect(sparklinePoints([3, 3], 100, 20)).toBe("0,10 100,10");
  });
  it("is empty with fewer than two points", () => {
    expect(sparklinePoints([], 100, 20)).toBe("");
    expect(sparklinePoints([1], 100, 20)).toBe("");
  });
  it("ignores NaN samples", () => {
    expect(sparklinePoints([0, NaN, 10], 100, 20)).toBe("0,20 100,0");
  });
});

describe("timeline", () => {
  const pts = [
    { time: 1000, value: 10 },
    { time: 2000, value: 30 },
    { time: 3000, value: 20 },
  ];
  it("scales time to x and value to y, and includes thresholds in the y range", () => {
    const t = timeline(pts, [40], { width: 100, height: 100 });
    expect(t.xMin).toBe(1000);
    expect(t.xMax).toBe(3000);
    expect(t.yMin).toBe(10);
    expect(t.yMax).toBe(40);
    expect(t.path).toBe("M0,100 L50,33.3 L100,66.7");
    expect(scaleY(40, t)).toBe(0);
  });
  it("pads a flat series so the line is not on the edge", () => {
    const t = timeline([{ time: 0, value: 5 }, { time: 10, value: 5 }], [], { width: 10, height: 10 });
    expect(t.yMin).toBeLessThan(5);
    expect(t.yMax).toBeGreaterThan(5);
  });
  it("produces evenly spaced time ticks", () => {
    expect(ticks(0, 4000, 4)).toEqual([0, 1000, 2000, 3000, 4000]);
  });
});

describe("timeline gaps", () => {
  const box = { width: 100, height: 100 };
  // samples every 1s, then silence from t=3s to t=9s, then one more at 9s
  const pts = [0, 1000, 2000, 3000, 9000].map((t) => ({ time: t, value: 1 }));

  it("extends the x axis to now when given", () => {
    const t = timeline(pts, [], box, { now: 10000 });
    expect(t.xMax).toBe(10000);
    expect(t.xMin).toBe(0);
  });

  it("breaks the line across a gap much longer than the usual spacing", () => {
    const t = timeline(pts, [], box, { now: 10000 });
    // one M per contiguous run: [0..3000] and [9000]
    expect(t.path.match(/M/g)).toHaveLength(2);
    expect(t.gaps).toEqual([{ from: 3000, to: 9000 }]);
  });

  it("reports a trailing gap when the last sample is stale", () => {
    const t = timeline(pts, [], box, { now: 20000 });
    expect(t.gaps).toEqual([{ from: 3000, to: 9000 }, { from: 9000, to: 20000 }]);
  });

  it("has no gaps for evenly spaced samples that are still fresh", () => {
    const even = [0, 1000, 2000, 3000].map((t) => ({ time: t, value: 1 }));
    expect(timeline(even, [], box, { now: 3500 }).gaps).toEqual([]);
    expect(timeline(even, [], box).gaps).toEqual([]);
  });

  it("uses an explicit gap threshold when given", () => {
    const even = [0, 1000, 2000, 3000].map((t) => ({ time: t, value: 1 }));
    expect(timeline(even, [], box, { now: 3500, gap: 400 }).gaps).toHaveLength(4);
  });

  it("treats a single sample as a run with no internal gap", () => {
    const t = timeline([{ time: 5000, value: 2 }], [], box, { now: 6000 });
    expect(t.path).toMatch(/^M/);
    expect(t.gaps).toEqual([]);
  });
});
