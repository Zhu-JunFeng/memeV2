import { describe, expect, it } from "vitest";
import { simulateBollingerPressureTrades } from "./bollingerStrategy.js";

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

describe("simulateBollingerPressureTrades", () => {
  it("marks buy on Bollinger pressure breakout and sell on take profit", () => {
    const klines = [
      bar(0, { open: 95, high: 98, low: 94, close: 96 }),
      bar(1, { open: 96, high: 100, low: 95, close: 99, volume: 500 }),
      bar(2, { open: 97, high: 101, low: 96, close: 100, volume: 500 }),
      bar(3, { open: 98, high: 102, low: 97, close: 101, volume: 500 }),
      bar(4, { open: 101, high: 104, low: 100, close: 104 }),
      bar(5, { open: 103, high: 112, low: 102, close: 111 }),
    ];
    const bands = klines.map((item) => ({
      time: item.time,
      middle: 99,
      upper: 102,
    }));

    const trades = simulateBollingerPressureTrades(klines, bands, {
      minTouches: 3,
      breakTolerance: 0.01,
      volumeWindow: 20,
      takeProfitRate: 0.08,
      feeRate: 0.015,
    });

    expect(trades).toHaveLength(1);
    expect(trades[0].buyPoint.time).toBe(5);
    expect(trades[0].buyPoint.price).toBeCloseTo(103.02, 8);
    expect(trades[0].sellPoint.time).toBe(6);
    expect(trades[0].outcome).toBe("take_profit");
    expect(trades[0].profitRate).toBeCloseTo(0.065, 8);
  });

  it("sells at fixed 5 percent stop loss when price drops", () => {
    const klines = [
      bar(0, { open: 95, high: 98, low: 94, close: 96 }),
      bar(1, { open: 96, high: 100, low: 95, close: 99, volume: 500 }),
      bar(2, { open: 97, high: 101, low: 96, close: 100, volume: 500 }),
      bar(3, { open: 98, high: 102, low: 97, close: 101, volume: 500 }),
      bar(4, { open: 101, high: 104, low: 97, close: 103.5 }),
      bar(5, { open: 103, high: 104, low: 96, close: 97 }),
    ];
    const bands = klines.map((item) => ({ time: item.time, middle: 99, upper: 102 }));

    const trades = simulateBollingerPressureTrades(klines, bands, {
      minTouches: 3,
      breakTolerance: 0.01,
      volumeWindow: 20,
      takeProfitRate: 0.08,
      feeRate: 0.015,
    });

    expect(trades).toHaveLength(1);
    expect(trades[0].outcome).toBe("stop_loss");
    expect(trades[0].sellPoint.price).toBeCloseTo(103.02 * 0.95, 8);
    expect(trades[0].profitRate).toBeCloseTo(-0.065, 8);
  });
});
