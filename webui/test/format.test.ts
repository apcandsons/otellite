import { describe, expect, it } from "vitest";
import { fmtDuration, fmtValue } from "../src/format.js";

describe("fmtValue", () => {
  it("appends a unit and rounds floats to 4 significant digits", () => {
    expect(fmtValue("163141061", "ms")).toBe("163141061 ms");
    expect(fmtValue("0.6452123", "")).toBe("0.6452");
  });
  it("omits the OTel dimensionless unit '1'", () => {
    expect(fmtValue("7882", "1")).toBe("7882");
    expect(fmtValue("0.9508693", "1")).toBe("0.9509");
  });
});

describe("fmtDuration", () => {
  it("uses the largest whole unit", () => {
    expect(fmtDuration(30)).toBe("30s");
    expect(fmtDuration(180)).toBe("3m");
    expect(fmtDuration(7200)).toBe("2h");
    expect(fmtDuration(90)).toBe("90s");
  });
});

describe("fmtAxis", () => {
  it("abbreviates large magnitudes and keeps small ones readable", async () => {
    const { fmtAxis } = await import("../src/format.js");
    expect(fmtAxis(514200000)).toBe("514.2M");
    expect(fmtAxis(42490000)).toBe("42.49M");
    expect(fmtAxis(7882)).toBe("7.882k");
    expect(fmtAxis(120)).toBe("120");
    expect(fmtAxis(0.8)).toBe("0.8");
    expect(fmtAxis(0.64521)).toBe("0.6452");
    expect(fmtAxis(-1500)).toBe("-1.5k");
    expect(fmtAxis(0)).toBe("0");
  });
});

describe("fmtValue large floats", () => {
  it("never uses exponent notation", () => {
    expect(fmtValue("10010.53", "s")).toBe("10011 s");
    expect(fmtValue("163141061.7", "ms")).toBe("163141062 ms");
    expect(fmtValue("999.99", "")).toBe("1000");
  });
});

describe("bytes", () => {
  it("shows byte values as KB / MB / GB", () => {
    expect(fmtValue("43122688", "By")).toBe("41.1 MB");
    expect(fmtValue("120000000", "Bytes")).toBe("114.4 MB");
    expect(fmtValue("512", "By")).toBe("512 B");
    expect(fmtValue("5368709120", "By")).toBe("5.0 GB");
  });
  it("shows byte thresholds and axis labels the same way", async () => {
    const { fmtAxis, fmtRule } = await import("../src/format.js");
    const mem = { path: "/iam/iam-api/metrics/go.memory.used.dat", op: ">" as const, threshold: 120000000, holdForSeconds: 30, channel: "ops" };
    expect(fmtRule(mem, "By")).toBe("> 114.4 MB for 30s");
    expect(fmtRule(mem)).toBe("> 120000000 for 30s");
    expect(fmtAxis(120000000, "By")).toBe("114.4 MB");
    expect(fmtAxis(120000000)).toBe("120M");
  });
  it("renders absent rules", async () => {
    const { fmtRule } = await import("../src/format.js");
    expect(fmtRule({ path: "/a/b/metrics/c.dat", op: "absent", threshold: 0, holdForSeconds: 30, channel: "ops" })).toBe("absent for 30s");
  });
});
