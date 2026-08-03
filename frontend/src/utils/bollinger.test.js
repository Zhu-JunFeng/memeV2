import { describe, expect, it } from "vitest";
import { calculatePandasTaBbands } from "./bollinger.js";

describe("calculatePandasTaBbands", () => {
  it("matches pandas_ta bbands sma/stdev ddof=0 semantics", () => {
    const items = [1, 2, 3, 4, 5].map((close, index) => ({
      time: index + 1,
      close,
    }));

    const bands = calculatePandasTaBbands(items, { length: 5, std: 2, ddof: 0 });

    expect(bands).toHaveLength(1);
    expect(bands[0].middle).toBeCloseTo(3, 8);
    expect(bands[0].upper).toBeCloseTo(5.8284271247, 8);
    expect(bands[0].lower).toBeCloseTo(0.1715728753, 8);
    expect(bands[0].bandwidth).toBeCloseTo(188.56180832, 8);
    expect(bands[0].percent).toBeCloseTo(0.85355339059, 8);
  });

  it("skips windows with invalid values", () => {
    const items = [1, Number.NaN, 3, 4, 5, 6].map((close, index) => ({
      time: index + 1,
      close,
    }));

    const bands = calculatePandasTaBbands(items, { length: 3, std: 2, ddof: 0 });

    expect(bands.map((item) => item.time)).toEqual([5, 6]);
  });
});
