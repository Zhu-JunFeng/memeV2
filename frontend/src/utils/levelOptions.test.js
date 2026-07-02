import { describe, expect, it } from "vitest";

import { buildLevelOptions } from "./levelOptions.js";

describe("buildLevelOptions", () => {
  it("maps fake pressure filter fields from percentages to ratios", () => {
    expect(
      buildLevelOptions({
        windowSize: 240,
        levelWindowSize: 240,
        levelWindowStep: 50,
        bandRangePercent: 2,
        minTouches: 3,
        confirmBars: 2,
        minWindowRangePercent: 8,
        minLevelSpacePercent: 6,
        minRetestPullbackPercent: 3,
        minRetestSpanBars: 4,
        retestLookbackBars: 720,
      }),
    ).toEqual({
      windowSize: 240,
      levelWindowSize: 240,
      levelWindowStep: 50,
      priceTolerance: 0.02,
      minTouches: 3,
      confirmBars: 2,
      minWindowRange: 0.08,
      minLevelSpace: 0.06,
      minRetestPullback: 0.03,
      minRetestSpanBars: 4,
      retestLookbackBars: 720,
    });
  });
});
