export const BOLLINGER_STRATEGY_DEFAULTS = Object.freeze({
  minTouches: 3,
  breakTolerance: 0.01,
  fixedStopLossRate: 0.05,
  takeProfitRate: 0.08,
  activationProfitRate: 0.07,
  lockedProfitRate: 0.04,
  feeRate: 0.015,
  positionSizeUsd: 10,
  volumeWindow: 20,
  volumeMultiplier: 1.2,
});

export function simulateBollingerPressureTrades(klines, bands, options = {}) {
  const config = { ...BOLLINGER_STRATEGY_DEFAULTS, ...options };
  const items = Array.isArray(klines) ? klines : [];
  if (!items.length || !Array.isArray(bands) || !bands.length) return [];

  const bandByTime = new Map(bands.map((band) => [band.time, band]));
  const touches = collectBollingerTouches(items, bandByTime, config);
  const trades = [];
  let activeExitIndex = -1;

  for (let endTouch = config.minTouches - 1; endTouch < touches.length; endTouch += 1) {
    const group = touches.slice(endTouch - config.minTouches + 1, endTouch + 1);
    const lastTouch = group[group.length - 1];
    if (!lastTouch || group[0].index <= activeExitIndex) continue;

    const frozenBand = bandByTime.get(lastTouch.time);
    if (!frozenBand || !Number.isFinite(frozenBand.upper)) continue;

    const breakoutThreshold = frozenBand.upper * (1 + config.breakTolerance);
    const breakoutIndex = findBreakoutIndex(items, lastTouch.index + 1, lastTouch.index + config.minTouches, breakoutThreshold, activeExitIndex);
    if (breakoutIndex < 0) continue;

    const buyPoint = {
      time: items[breakoutIndex].time,
      openTime: items[breakoutIndex].openTime,
      price: breakoutThreshold,
    };
    const exit = simulateExit(items, breakoutIndex, buyPoint.price, config);
    if (!exit) continue;

    const grossProfitRate = profitRate(buyPoint.price, exit.price);
    const netProfitRate = grossProfitRate - config.feeRate;
    trades.push({
      id: `boll-${breakoutIndex}-${exit.index}`,
      buyPoint,
      sellPoint: {
        time: items[exit.index].time,
        openTime: items[exit.index].openTime,
        price: exit.price,
      },
      outcome: exit.outcome,
      exitReason: exit.reason,
      holdingBars: exit.index - breakoutIndex,
      grossProfitRate,
      profitRate: netProfitRate,
      profitUsd: config.positionSizeUsd * grossProfitRate - config.positionSizeUsd * config.feeRate,
      feeRate: config.feeRate,
      positionSizeUsd: config.positionSizeUsd,
      level: {
        lower: frozenBand.middle,
        upper: frozenBand.upper,
        breakoutThreshold,
        frozenAt: lastTouch.openTime,
      },
      touches: group.map((touch) => ({
        time: touch.time,
        openTime: touch.openTime,
        price: touch.price,
      })),
    });
    activeExitIndex = exit.index;
  }

  return trades;
}

export function collectBollingerTouches(klines, bandByTime, config = BOLLINGER_STRATEGY_DEFAULTS) {
  return klines
    .map((item, index) => {
      const band = bandByTime.get(item.time);
      if (!band || !isBullish(item)) return null;
      if (!Number.isFinite(band.middle) || !Number.isFinite(band.upper)) return null;
      if (item.high < band.middle || item.high > band.upper) return null;
      if (!touchVolumeConfirmed(klines, index, config)) return null;
      return {
        index,
        time: item.time,
        openTime: item.openTime,
        price: item.high,
      };
    })
    .filter(Boolean);
}

function findBreakoutIndex(klines, start, end, threshold, activeExitIndex) {
  const last = Math.min(end, klines.length - 1);
  for (let index = Math.max(start, activeExitIndex + 1); index <= last; index += 1) {
    if (klines[index].close > threshold) return index;
  }
  return -1;
}

function simulateExit(klines, entryIndex, entryPrice, config) {
  if (entryIndex < 0 || entryIndex >= klines.length || entryPrice <= 0) return null;
  const initialStopLoss = entryPrice * (1 - config.fixedStopLossRate);
  const takeProfitPrice = entryPrice * (1 + config.takeProfitRate);
  let trailingStopPrice = entryPrice * (1 + config.lockedProfitRate);
  let trailingArmed = false;

  for (let index = entryIndex + 1; index < klines.length; index += 1) {
    const item = klines[index];
    if (trailingArmed && item.low <= trailingStopPrice) {
      return {
        index,
        price: trailingStopPrice,
        outcome: "stop_loss",
        reason: "盈利达到触发阈值后回撤到锁定收益率，执行动态止损",
      };
    }
    if (item.low <= initialStopLoss) {
      return {
        index,
        price: initialStopLoss,
        outcome: "stop_loss",
        reason: "买入后跌幅达到固定 5% 止损，按止损价卖出",
      };
    }
    if (item.high >= takeProfitPrice) {
      return {
        index,
        price: takeProfitPrice,
        outcome: "take_profit",
        reason: "达到止盈比例，按止盈价卖出",
      };
    }
    const next = steppedTrailingStopPrice(entryPrice, item.high, config, trailingStopPrice, trailingArmed);
    trailingStopPrice = next.price;
    trailingArmed = next.armed;
  }

  const lastIndex = klines.length - 1;
  return {
    index: lastIndex,
    price: klines[lastIndex].close || klines[lastIndex].open,
    outcome: "timeout",
    reason: "直到样本结束仍未触发止盈或止损，按最后一根 K 线收盘卖出",
  };
}

function steppedTrailingStopPrice(entryPrice, highPrice, config, currentStop, armed) {
  if (entryPrice <= 0 || highPrice <= 0 || highPrice < entryPrice * (1 + config.activationProfitRate)) {
    return { price: currentStop, armed };
  }
  const lockStepRate = 0.01;
  let highestProfitRate = profitRate(entryPrice, highPrice);
  if (config.takeProfitRate > 0 && highestProfitRate > config.takeProfitRate) {
    highestProfitRate = config.takeProfitRate;
  }
  const steps = Math.max(0, Math.floor((highestProfitRate - config.activationProfitRate + 1e-9) / lockStepRate));
  let lockRate = config.lockedProfitRate + steps * lockStepRate;
  if (config.takeProfitRate > config.activationProfitRate) {
    const maxLockRate = config.lockedProfitRate + config.takeProfitRate - config.activationProfitRate;
    if (lockRate > maxLockRate) lockRate = maxLockRate;
  }
  const nextStop = entryPrice * (1 + lockRate);
  return { price: !armed || nextStop > currentStop ? nextStop : currentStop, armed: true };
}

function touchVolumeConfirmed(klines, index, config) {
  const baselineVolume = rollingAverageVolumeBefore(klines, index, config.volumeWindow);
  if (baselineVolume <= 0) return true;
  return klines[index].volume >= baselineVolume * Math.max(config.volumeMultiplier, 1.35);
}

function rollingAverageVolumeBefore(klines, index, windowSize) {
  const start = Math.max(0, index - windowSize);
  const samples = klines.slice(start, index).map((item) => Number(item.volume || 0)).filter((value) => value > 0);
  if (!samples.length) return 0;
  return samples.reduce((total, value) => total + value, 0) / samples.length;
}

function isBullish(item) {
  return item.close > item.open;
}

function profitRate(entryPrice, exitPrice) {
  if (entryPrice <= 0) return 0;
  return (exitPrice - entryPrice) / entryPrice;
}
