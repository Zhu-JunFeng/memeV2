import { describe, expect, it } from "vitest";

import { summarizeOpenPositions } from "./positions.js";

describe("summarizeOpenPositions", () => {
  it("only includes current open positions", () => {
    const result = summarizeOpenPositions([
      {
        status: "open",
        costAmount: 19.97,
        marketValue: 21.24,
        unrealizedPnl: 1.27,
      },
      {
        status: "closed",
        costAmount: 20.21,
        marketValue: 0,
        realizedPnl: 0.27,
      },
      {
        status: "closed",
        costAmount: 20.51,
        marketValue: 0,
        realizedPnl: 0.46,
      },
    ]);

    expect(result).toEqual({ cost: 19.97, marketValue: 21.24, totalPnl: 1.27 });
  });

  it("derives open market value when the stored value is unavailable", () => {
    const result = summarizeOpenPositions([
      { status: "open", costAmount: 20, marketValue: 0, unrealizedPnl: -2.5 },
    ]);

    expect(result).toEqual({ cost: 20, marketValue: 17.5, totalPnl: -2.5 });
  });
});
