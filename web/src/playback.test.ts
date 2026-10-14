import { describe, it, expect } from "vitest";
import { reconcile, step, type Scan } from "./playback";
const a = { id: "a", state: "ready" } as Scan,
  b = { id: "b", state: "ready" } as Scan;
describe("timeline", () => {
  it("live follows newest ready observation", () =>
    expect(reconcile([a, b], "a", true)).toBe("b"));
  it("history stays selected when observations arrive", () =>
    expect(reconcile([a, b], "a", false)).toBe("a"));
  it("does not silently replace expired history", () =>
    expect(reconcile([b], "a", false)).toBe("a"));
  it("handles empty and single-frame history", () => {
    expect(step([], "", 1)).toBe("");
    expect(step([a], "a", 1)).toBe("a");
  });
  it("wraps actual irregular observations", () => {
    expect(step([a, b], "b", 1)).toBe("a");
    expect(step([a, b], "a", -1)).toBe("b");
  });
});
