import { describe, expect, it } from "vitest";
import {
  analyzeHorizontalPressure,
  analyzeHorizontalPressureReplay,
  buildResistanceClusters,
  findPivotHighs,
  simulateHorizontalPressureTrades,
} from "./horizontalPressure.js";

function bar(index, values) {
  return {
    time: index + 1,
    openTime: `2026-07-23T00:${String(index).padStart(2, "0")}:00+08:00`,
    open: values.open,
    high: values.high,
    low: values.low,
    close: values.close,
    volume: values.volume ?? 100,
  };
}

describe("horizontal pressure analysis", () => {
  it("finds pivot highs and clusters nearby horizontal resistance", () => {
    const klines = [
      bar(0, { open: 90, high: 95, low: 88, close: 92 }),
      bar(1, { open: 94, high: 101, low: 93, close: 98 }),
      bar(2, { open: 96, high: 99, low: 94, close: 95 }),
      bar(3, { open: 94, high: 100.8, low: 93, close: 98 }),
      bar(4, { open: 97, high: 99, low: 94, close: 95 }),
      bar(5, { open: 95, high: 101.2, low: 94, close: 98 }),
      bar(6, { open: 96, high: 99, low: 94, close: 95 }),
    ];

    const pivots = findPivotHighs(klines, 1);
    const levels = buildResistanceClusters(pivots, klines, {
      pivotWindow: 1,
      mergePercent: 0.01,
      minTouches: 2,
      maxLevels: 4,
      volumeWindow: 20,
    });

    expect(pivots.map((item) => item.time)).toEqual([2, 4, 6]);
    expect(levels).toHaveLength(1);
    expect(levels[0].pivotCount).toBe(3);
    expect(levels[0].center).toBeCloseTo(101, 8);
  });

  it("simulates B/S after repeated horizontal pressure touches", () => {
    const klines = [
      bar(0, { open: 90, high: 96, low: 88, close: 92 }),
      bar(1, { open: 95, high: 100.1, low: 94, close: 99, volume: 500 }),
      bar(2, { open: 96, high: 98, low: 94, close: 95 }),
      bar(3, { open: 96, high: 100.2, low: 95, close: 99, volume: 500 }),
      bar(4, { open: 97, high: 99, low: 94, close: 95 }),
      bar(5, { open: 97, high: 100.3, low: 96, close: 99, volume: 500 }),
      bar(6, { open: 99, high: 104, low: 98, close: 102.5 }),
      bar(7, { open: 102, high: 112, low: 101, close: 111 }),
    ];
    const levels = [{
      id: 1,
      center: 100.2,
      lower: 99.8,
      upper: 100.4,
      score: 10,
      touches: [
        { index: 1, time: 2, openTime: klines[1].openTime, price: 100.1 },
        { index: 3, time: 4, openTime: klines[3].openTime, price: 100.2 },
        { index: 5, time: 6, openTime: klines[5].openTime, price: 100.3 },
      ],
    }];

    const trades = simulateHorizontalPressureTrades(klines, levels, {
      minTouches: 3,
      breakTolerance: 0.01,
      takeProfitRate: 0.08,
      feeRate: 0.015,
    });

    expect(trades).toHaveLength(1);
    expect(trades[0].buyPoint.time).toBe(7);
    expect(trades[0].sellPoint.time).toBe(8);
    expect(trades[0].outcome).toBe("take_profit");
  });

  it("applies position size and buy/sell slippage to trade PnL", () => {
    const klines = [
      bar(0, { open: 90, high: 96, low: 88, close: 92 }),
      bar(1, { open: 95, high: 100.1, low: 94, close: 99, volume: 500 }),
      bar(2, { open: 96, high: 98, low: 94, close: 95 }),
      bar(3, { open: 96, high: 100.2, low: 95, close: 99, volume: 500 }),
      bar(4, { open: 97, high: 99, low: 94, close: 95 }),
      bar(5, { open: 97, high: 100.3, low: 96, close: 99, volume: 500 }),
      bar(6, { open: 99, high: 104, low: 98, close: 102.5 }),
      bar(7, { open: 102, high: 112, low: 101, close: 111 }),
    ];
    const levels = [{
      id: 1,
      center: 100.2,
      lower: 99.8,
      upper: 100.4,
      score: 10,
      touches: [
        { index: 1, time: 2, openTime: klines[1].openTime, price: 100.1 },
        { index: 3, time: 4, openTime: klines[3].openTime, price: 100.2 },
        { index: 5, time: 6, openTime: klines[5].openTime, price: 100.3 },
      ],
    }];

    const trades = simulateHorizontalPressureTrades(klines, levels, {
      minTouches: 3,
      breakTolerance: 0.01,
      takeProfitRate: 0.08,
      positionSizeUsd: 100,
      buySlippageRate: 0.01,
      sellSlippageRate: 0.02,
      feeRate: 0.015,
    });

    expect(trades).toHaveLength(1);
    expect(trades[0].buyPoint.price).toBeCloseTo(100.4 * 1.01 * 1.01, 8);
    expect(trades[0].sellPoint.price).toBeCloseTo(trades[0].sellPoint.triggerPrice * 0.98, 8);
    expect(trades[0].profitUsd).toBeCloseTo(100 * trades[0].profitRate, 8);
  });

  it("returns an end-to-end analysis object", () => {
    const klines = Array.from({ length: 12 }, (_, index) =>
      bar(index, {
        open: 100 + (index % 2),
        high: index % 3 === 1 ? 110 : 104 + (index % 2),
        low: 96,
        close: 101 + (index % 2),
        volume: 200,
      }),
    );

    const result = analyzeHorizontalPressure(klines, { pivotWindow: 1, mergePercent: 0.02, minTouches: 2 });

    expect(result).toHaveProperty("pivots");
    expect(result).toHaveProperty("levels");
    expect(result).toHaveProperty("trades");
  });

  it("confirms pivot highs only after right-side bars are available in replay mode", () => {
    const klines = [
      bar(0, { open: 90, high: 94, low: 88, close: 92 }),
      bar(1, { open: 92, high: 96, low: 90, close: 94 }),
      bar(2, { open: 95, high: 110, low: 94, close: 101 }),
      bar(3, { open: 99, high: 104, low: 95, close: 98 }),
      bar(4, { open: 98, high: 103, low: 94, close: 97 }),
      bar(5, { open: 97, high: 102, low: 94, close: 96 }),
    ];

    const result = analyzeHorizontalPressureReplay(klines, { pivotWindow: 2, minTouches: 2 });

    expect(result.pivots).toHaveLength(1);
    expect(result.pivots[0].index).toBe(2);
    expect(result.pivots[0].confirmedIndex).toBe(4);
  });

  it("replays B/S without using future pressure levels", () => {
    const klines = [
      bar(0, { open: 90, high: 96, low: 88, close: 92 }),
      bar(1, { open: 95, high: 100.1, low: 94, close: 99, volume: 500 }),
      bar(2, { open: 96, high: 98, low: 94, close: 95 }),
      bar(3, { open: 96, high: 100.2, low: 95, close: 99, volume: 500 }),
      bar(4, { open: 97, high: 99, low: 94, close: 95 }),
      bar(5, { open: 97, high: 100.3, low: 96, close: 99, volume: 500 }),
      bar(6, { open: 99, high: 104, low: 98, close: 102.5 }),
      bar(7, { open: 102, high: 112, low: 101, close: 111 }),
    ];

    const result = analyzeHorizontalPressureReplay(klines, {
      pivotWindow: 1,
      mergePercent: 0.01,
      minTouches: 3,
      breakTolerance: 0.01,
      takeProfitRate: 0.08,
      feeRate: 0.015,
    });

    expect(result.trades).toHaveLength(1);
    expect(result.trades[0].buyIndex).toBe(6);
    expect(result.trades[0].level.pivots.every((pivot) => pivot.confirmedIndex <= result.trades[0].buyIndex)).toBe(true);
    expect(result.trades[0].touches.every((touch) => touch.index < result.trades[0].buyIndex)).toBe(true);
    expect(result.trades[0].outcome).toBe("take_profit");
  });

  it("does not buy when a historical level would only be formed after the breakout", () => {
    const klines = [
      bar(0, { open: 90, high: 96, low: 88, close: 92 }),
      bar(1, { open: 95, high: 100.1, low: 94, close: 99, volume: 500 }),
      bar(2, { open: 96, high: 99.8, low: 94, close: 99, volume: 500 }),
      bar(3, { open: 100, high: 104, low: 99, close: 103 }),
      bar(4, { open: 99, high: 100.2, low: 94, close: 98, volume: 500 }),
      bar(5, { open: 98, high: 99, low: 94, close: 95 }),
      bar(6, { open: 96, high: 100.3, low: 95, close: 99, volume: 500 }),
      bar(7, { open: 98, high: 99, low: 94, close: 95 }),
      bar(8, { open: 96, high: 100.4, low: 95, close: 99, volume: 500 }),
      bar(9, { open: 98, high: 99, low: 94, close: 95 }),
    ];

    const result = analyzeHorizontalPressureReplay(klines, {
      pivotWindow: 1,
      mergePercent: 0.01,
      minTouches: 3,
      breakTolerance: 0.01,
    });

    expect(result.trades).toHaveLength(0);
  });
});
