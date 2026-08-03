export const HORIZONTAL_PRESSURE_DEFAULTS = Object.freeze({
  pivotWindow: 3,
  mergePercent: 0.008,
  minTouches: 3,
  breakTolerance: 0.01,
  maxLevels: 18,
  fixedStopLossRate: 0.05,
  takeProfitRate: 0.08,
  activationProfitRate: 0.07,
  lockedProfitRate: 0.04,
  feeRate: 0.015,
  positionSizeUsd: 10,
  buySlippageRate: 0,
  sellSlippageRate: 0,
  volumeWindow: 20,
  volumeMultiplier: 1.2,
});

export function analyzeHorizontalPressure(klines, options = {}) {
  const config = { ...HORIZONTAL_PRESSURE_DEFAULTS, ...options };
  const items = Array.isArray(klines) ? klines : [];
  const pivots = findPivotHighs(items, config.pivotWindow);
  const levels = buildResistanceClusters(pivots, items, config);
  const trades = simulateHorizontalPressureTrades(items, levels, config);
  return { mode: "historical", pivots, levels, trades };
}

export function analyzeHorizontalPressureReplay(klines, options = {}) {
  const config = { ...HORIZONTAL_PRESSURE_DEFAULTS, ...options };
  const items = Array.isArray(klines) ? klines : [];
  const confirmedPivots = [];
  const clusters = [];
  const trades = [];
  let latestLevels = [];
  let position = null;
  let lastExitIndex = -1;

  for (let index = 0; index < items.length; index += 1) {
    const confirmedPivot = confirmPivotHighAt(items, index, config.pivotWindow);
    if (confirmedPivot) {
      confirmedPivots.push(confirmedPivot);
      upsertReplayPivotCluster(clusters, confirmedPivot, items, index, config);
    } else if (clusters.length) {
      updateReplayClusterTouches(clusters, items, index - 1, config);
    }
    latestLevels = rankReplayClusterLevels(clusters, config);

    if (position && index > position.buyIndex) {
      const exit = evaluateExitAtBar(items[index], index, position, config);
      if (exit) {
        const trade = closeReplayTrade(position, items, exit, config);
        trades.push(trade);
        lastExitIndex = index;
        position = null;
        continue;
      }
    }

    if (position || index <= lastExitIndex || confirmedPivots.length <= 0) continue;

    const entry = findReplayEntry(items, index, latestLevels, config);
    if (!entry) continue;

    position = {
      ...entry,
      buyIndex: index,
      buyPoint: {
        time: items[index].time,
        openTime: items[index].openTime,
        price: entry.entryPrice,
        triggerPrice: entry.triggerPrice,
      },
      trailingStopPrice: entry.entryPrice * (1 + config.lockedProfitRate),
      trailingArmed: false,
    };
  }

  if (position && items.length) {
    const lastIndex = items.length - 1;
    const last = items[lastIndex];
    trades.push(closeReplayTrade(position, items, {
      index: lastIndex,
      price: Number(last?.close || last?.open || position.entryPrice),
      outcome: "timeout",
      reason: "样本结束",
    }, config));
  }

  const usedLevels = dedupeReplayLevels(trades.map((trade) => trade.level));
  const levels = dedupeReplayLevels([...usedLevels, ...latestLevels]).slice(0, config.maxLevels);
  return {
    mode: "replay",
    pivots: confirmedPivots,
    levels: levels.map((level, index) => ({ ...level, rank: index + 1 })),
    latestLevels,
    trades,
  };
}

export function findPivotHighs(klines, pivotWindow = HORIZONTAL_PRESSURE_DEFAULTS.pivotWindow) {
  const window = Math.max(1, Number.parseInt(pivotWindow, 10) || HORIZONTAL_PRESSURE_DEFAULTS.pivotWindow);
  const pivots = [];
  for (let index = window; index < klines.length - window; index += 1) {
    const item = klines[index];
    const high = Number(item?.high);
    if (!Number.isFinite(high) || high <= 0) continue;
    let leftMax = -Infinity;
    let rightMax = -Infinity;
    for (let cursor = index - window; cursor < index; cursor += 1) {
      leftMax = Math.max(leftMax, Number(klines[cursor]?.high || 0));
    }
    for (let cursor = index + 1; cursor <= index + window; cursor += 1) {
      rightMax = Math.max(rightMax, Number(klines[cursor]?.high || 0));
    }
    if (high > leftMax && high >= rightMax) {
      pivots.push({
        index,
        time: item.time,
        openTime: item.openTime,
        price: high,
        volume: Number(item.volume || 0),
      });
    }
  }
  return pivots;
}

export function buildResistanceClusters(pivots, klines, options = {}) {
  const config = { ...HORIZONTAL_PRESSURE_DEFAULTS, ...options };
  const mergePercent = Math.max(0.0001, Number(config.mergePercent || HORIZONTAL_PRESSURE_DEFAULTS.mergePercent));
  const clusters = [];
  [...pivots]
    .sort((left, right) => left.price - right.price)
    .forEach((pivot) => {
      let bestCluster = null;
      let bestDistance = Infinity;
      clusters.forEach((cluster) => {
        const distance = Math.abs(pivot.price - cluster.center) / cluster.center;
        if (distance <= mergePercent && distance < bestDistance) {
          bestCluster = cluster;
          bestDistance = distance;
        }
      });
      if (!bestCluster) {
        clusters.push({ pivots: [pivot], center: pivot.price });
        return;
      }
      bestCluster.pivots.push(pivot);
      bestCluster.center = median(bestCluster.pivots.map((item) => item.price));
    });

  const levels = clusters
    .map((cluster, index) => buildLevelFromCluster(cluster, index + 1, klines, config))
    .filter((level) => level.pivotCount >= 2 || level.touchCount >= config.minTouches)
    .sort((left, right) => right.score - left.score)
    .slice(0, config.maxLevels)
    .map((level, index) => ({ ...level, rank: index + 1 }));
  return levels;
}

export function simulateHorizontalPressureTrades(klines, levels, options = {}) {
  const config = { ...HORIZONTAL_PRESSURE_DEFAULTS, ...options };
  const candidates = [];
  levels.forEach((level) => {
    for (let endTouch = config.minTouches - 1; endTouch < level.touches.length; endTouch += 1) {
      const group = level.touches.slice(endTouch - config.minTouches + 1, endTouch + 1);
      const lastTouch = group[group.length - 1];
      const threshold = level.upper * (1 + config.breakTolerance);
      const breakoutIndex = findBreakoutIndex(klines, lastTouch.index + 1, lastTouch.index + config.minTouches, threshold);
      if (breakoutIndex < 0) continue;
      const entryPrice = applyBuySlippage(threshold, config);
      const exit = simulateExit(klines, breakoutIndex, entryPrice, config);
      if (!exit) continue;
      const sellPrice = applySellSlippage(exit.price, config);
      const grossProfitRate = profitRate(entryPrice, sellPrice);
      candidates.push({
        id: `horizontal-${level.id}-${breakoutIndex}-${exit.index}`,
        level: { ...level, breakoutThreshold: threshold },
        touches: group,
        buyIndex: breakoutIndex,
        sellIndex: exit.index,
        buyPoint: {
          time: klines[breakoutIndex].time,
          openTime: klines[breakoutIndex].openTime,
          price: entryPrice,
          triggerPrice: threshold,
        },
        sellPoint: {
          time: klines[exit.index].time,
          openTime: klines[exit.index].openTime,
          price: sellPrice,
          triggerPrice: exit.price,
        },
        outcome: exit.outcome,
        exitReason: exit.reason,
        holdingBars: exit.index - breakoutIndex,
        grossProfitRate,
        profitRate: grossProfitRate - config.feeRate,
        profitUsd: config.positionSizeUsd * grossProfitRate - config.positionSizeUsd * config.feeRate,
        feeRate: config.feeRate,
        positionSizeUsd: config.positionSizeUsd,
        buySlippageRate: config.buySlippageRate,
        sellSlippageRate: config.sellSlippageRate,
        score: level.score,
      });
    }
  });

  const selected = [];
  let activeExitIndex = -1;
  const seenBuyTimes = new Set();
  candidates
    .sort((left, right) => left.buyIndex - right.buyIndex || right.score - left.score)
    .forEach((trade) => {
      if (trade.buyIndex <= activeExitIndex) return;
      if (seenBuyTimes.has(trade.buyPoint.time)) return;
      selected.push(trade);
      seenBuyTimes.add(trade.buyPoint.time);
      activeExitIndex = trade.sellIndex;
    });
  return selected;
}

function buildLevelFromCluster(cluster, id, klines, config) {
  const prices = cluster.pivots.map((item) => item.price).sort((left, right) => left - right);
  const center = median(prices);
  const halfWidth = center * config.mergePercent * 0.5;
  const lower = Math.min(prices[0], center - halfWidth);
  const upper = Math.max(prices[prices.length - 1], center + halfWidth);
  const touches = collectTouches(klines, lower, upper, config);
  const pivotVolume = average(cluster.pivots.map((item) => item.volume));
  const touchVolume = average(touches.map((item) => item.volume));
  const lastIndex = Math.max(...cluster.pivots.map((item) => item.index), ...touches.map((item) => item.index), 0);
  const recencyScore = klines.length ? lastIndex / klines.length : 0;
  const score =
    cluster.pivots.length * 2 +
    touches.length * 1.5 +
    Math.log10(Math.max(1, pivotVolume + touchVolume)) * 0.6 +
    recencyScore * 2;
  return {
    id,
    center,
    lower,
    upper,
    pivotCount: cluster.pivots.length,
    touchCount: touches.length,
    pivots: cluster.pivots,
    touches,
    score,
    lastTime: klines[lastIndex]?.openTime || cluster.pivots.at(-1)?.openTime || "",
  };
}

function collectTouches(klines, lower, upper, config) {
  return klines
    .map((item, index) => {
      if (!isBullish(item)) return null;
      if (item.high < lower || item.high > upper) return null;
      if (item.close > upper) return null;
      if (!touchVolumeConfirmed(klines, index, config)) return null;
      return {
        index,
        time: item.time,
        openTime: item.openTime,
        price: item.high,
        volume: Number(item.volume || 0),
      };
    })
    .filter(Boolean);
}

function findBreakoutIndex(klines, start, end, threshold) {
  const last = Math.min(end, klines.length - 1);
  for (let index = Math.max(0, start); index <= last; index += 1) {
    if (klines[index].close > threshold) return index;
  }
  return -1;
}

function confirmPivotHighAt(klines, currentIndex, pivotWindow) {
  const window = Math.max(1, Number.parseInt(pivotWindow, 10) || HORIZONTAL_PRESSURE_DEFAULTS.pivotWindow);
  const pivotIndex = currentIndex - window;
  if (pivotIndex < window || currentIndex >= klines.length) return null;
  const item = klines[pivotIndex];
  const high = Number(item?.high);
  if (!Number.isFinite(high) || high <= 0) return null;

  let leftMax = -Infinity;
  let rightMax = -Infinity;
  for (let cursor = pivotIndex - window; cursor < pivotIndex; cursor += 1) {
    leftMax = Math.max(leftMax, Number(klines[cursor]?.high || 0));
  }
  for (let cursor = pivotIndex + 1; cursor <= currentIndex; cursor += 1) {
    rightMax = Math.max(rightMax, Number(klines[cursor]?.high || 0));
  }
  if (!(high > leftMax && high >= rightMax)) return null;
  return {
    index: pivotIndex,
    time: item.time,
    openTime: item.openTime,
    price: high,
    volume: Number(item.volume || 0),
    confirmedIndex: currentIndex,
    confirmedTime: klines[currentIndex]?.time,
    confirmedOpenTime: klines[currentIndex]?.openTime,
  };
}

function findReplayEntry(klines, index, levels, config) {
  const item = klines[index];
  if (!item || !Number.isFinite(Number(item.close))) return null;

  for (const level of levels) {
    const touches = level.touches || [];
    if (touches.length < config.minTouches) continue;
    const group = touches.slice(-config.minTouches);
    const lastTouch = group[group.length - 1];
    if (!lastTouch || lastTouch.index >= index) continue;
    if (index - lastTouch.index > config.minTouches) continue;

    const threshold = level.upper * (1 + config.breakTolerance);
    if (item.close <= threshold) continue;
    const entryPrice = applyBuySlippage(threshold, config);
    return {
      entryPrice,
      triggerPrice: threshold,
      level: {
        ...level,
        replayGeneratedAtIndex: index,
        replayGeneratedAtTime: item.time,
        replayGeneratedAtOpenTime: item.openTime,
        breakoutThreshold: threshold,
      },
      touches: group,
    };
  }
  return null;
}

function upsertReplayPivotCluster(clusters, pivot, klines, currentIndex, config) {
  const mergePercent = Math.max(0.0001, Number(config.mergePercent || HORIZONTAL_PRESSURE_DEFAULTS.mergePercent));
  let bestCluster = null;
  let bestDistance = Infinity;
  clusters.forEach((cluster) => {
    const distance = Math.abs(pivot.price - cluster.center) / cluster.center;
    if (distance <= mergePercent && distance < bestDistance) {
      bestCluster = cluster;
      bestDistance = distance;
    }
  });

  if (!bestCluster) {
    bestCluster = { id: clusters.length + 1, pivots: [], center: pivot.price, level: null };
    clusters.push(bestCluster);
  }

  bestCluster.pivots.push(pivot);
  bestCluster.center = median(bestCluster.pivots.map((item) => item.price));
  bestCluster.level = buildLevelFromCluster(bestCluster, bestCluster.id, klines.slice(0, currentIndex), config);
}

function updateReplayClusterTouches(clusters, klines, touchIndex, config) {
  if (touchIndex < 0) return;
  clusters.forEach((cluster) => {
    const level = cluster.level;
    if (!level) return;
    if ((level.touches || []).some((touch) => touch.index === touchIndex)) return;
    const touch = collectTouchForLevel(klines, touchIndex, level.lower, level.upper, config);
    if (!touch) return;
    cluster.level = {
      ...level,
      touches: [...(level.touches || []), touch],
      touchCount: Number(level.touchCount || 0) + 1,
      score: Number(level.score || 0) + 1.5,
      lastTime: klines[touchIndex]?.openTime || level.lastTime,
    };
  });
}

function rankReplayClusterLevels(clusters, config) {
  return clusters
    .map((cluster) => cluster.level)
    .filter((level) => level && (level.pivotCount >= 2 || level.touchCount >= config.minTouches))
    .sort((left, right) => Number(right.score || 0) - Number(left.score || 0))
    .slice(0, config.maxLevels)
    .map((level, index) => ({ ...level, rank: index + 1 }));
}

function collectTouchForLevel(klines, index, lower, upper, config) {
  const item = klines[index];
  if (!item || !isBullish(item)) return null;
  if (item.high < lower || item.high > upper) return null;
  if (item.close > upper) return null;
  if (!touchVolumeConfirmed(klines, index, config)) return null;
  return {
    index,
    time: item.time,
    openTime: item.openTime,
    price: item.high,
    volume: Number(item.volume || 0),
  };
}

function evaluateExitAtBar(item, index, position, config) {
  const entryPrice = position.entryPrice;
  const initialStopLoss = entryPrice * (1 - config.fixedStopLossRate);
  const takeProfitPrice = entryPrice * (1 + config.takeProfitRate);

  if (position.trailingArmed && item.low <= position.trailingStopPrice) {
    return { index, price: position.trailingStopPrice, outcome: "stop_loss", reason: "动态止损" };
  }
  if (item.low <= initialStopLoss) {
    return { index, price: initialStopLoss, outcome: "stop_loss", reason: "固定 5% 止损" };
  }
  if (item.high >= takeProfitPrice) {
    return { index, price: takeProfitPrice, outcome: "take_profit", reason: "固定 8% 止盈" };
  }

  const next = steppedTrailingStopPrice(entryPrice, item.high, config, position.trailingStopPrice, position.trailingArmed);
  position.trailingStopPrice = next.price;
  position.trailingArmed = next.armed;
  return null;
}

function closeReplayTrade(position, klines, exit, config) {
  const sellPrice = applySellSlippage(exit.price, config);
  const grossProfitRate = profitRate(position.entryPrice, sellPrice);
  return {
    id: `horizontal-replay-${position.level.id}-${position.buyIndex}-${exit.index}`,
    level: position.level,
    touches: position.touches,
    buyIndex: position.buyIndex,
    sellIndex: exit.index,
    buyPoint: position.buyPoint,
    sellPoint: {
      time: klines[exit.index]?.time,
      openTime: klines[exit.index]?.openTime,
      price: sellPrice,
      triggerPrice: exit.price,
    },
    outcome: exit.outcome,
    exitReason: exit.reason,
    holdingBars: exit.index - position.buyIndex,
    grossProfitRate,
    profitRate: grossProfitRate - config.feeRate,
    profitUsd: config.positionSizeUsd * grossProfitRate - config.positionSizeUsd * config.feeRate,
    feeRate: config.feeRate,
    positionSizeUsd: config.positionSizeUsd,
    buySlippageRate: config.buySlippageRate,
    sellSlippageRate: config.sellSlippageRate,
    score: position.level.score,
    replay: true,
  };
}

function dedupeReplayLevels(levels) {
  const byKey = new Map();
  levels.filter(Boolean).forEach((level) => {
    const key = [
      Math.round(Number(level.lower || 0) * 100),
      Math.round(Number(level.upper || 0) * 100),
      level.replayGeneratedAtIndex ?? "latest",
    ].join("-");
    if (!byKey.has(key) || Number(level.score || 0) > Number(byKey.get(key).score || 0)) {
      byKey.set(key, level);
    }
  });
  return [...byKey.values()].sort((left, right) => Number(right.score || 0) - Number(left.score || 0));
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
      return { index, price: trailingStopPrice, outcome: "stop_loss", reason: "动态止损" };
    }
    if (item.low <= initialStopLoss) {
      return { index, price: initialStopLoss, outcome: "stop_loss", reason: "固定 5% 止损" };
    }
    if (item.high >= takeProfitPrice) {
      return { index, price: takeProfitPrice, outcome: "take_profit", reason: "固定 8% 止盈" };
    }
    const next = steppedTrailingStopPrice(entryPrice, item.high, config, trailingStopPrice, trailingArmed);
    trailingStopPrice = next.price;
    trailingArmed = next.armed;
  }
  const lastIndex = klines.length - 1;
  return { index: lastIndex, price: klines[lastIndex].close || klines[lastIndex].open, outcome: "timeout", reason: "样本结束" };
}

function steppedTrailingStopPrice(entryPrice, highPrice, config, currentStop, armed) {
  if (entryPrice <= 0 || highPrice <= 0 || highPrice < entryPrice * (1 + config.activationProfitRate)) {
    return { price: currentStop, armed };
  }
  const lockStepRate = 0.01;
  let highestProfitRate = profitRate(entryPrice, highPrice);
  if (config.takeProfitRate > 0 && highestProfitRate > config.takeProfitRate) highestProfitRate = config.takeProfitRate;
  const steps = Math.max(0, Math.floor((highestProfitRate - config.activationProfitRate + 1e-9) / lockStepRate));
  let lockRate = config.lockedProfitRate + steps * lockStepRate;
  if (config.takeProfitRate > config.activationProfitRate) {
    lockRate = Math.min(lockRate, config.lockedProfitRate + config.takeProfitRate - config.activationProfitRate);
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
  return average(samples);
}

function isBullish(item) {
  return Number(item?.close) > Number(item?.open);
}

function median(values) {
  if (!values.length) return 0;
  const sorted = [...values].sort((left, right) => left - right);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function average(values) {
  const valid = values.filter((value) => Number.isFinite(Number(value)));
  if (!valid.length) return 0;
  return valid.reduce((total, value) => total + Number(value), 0) / valid.length;
}

function profitRate(entryPrice, exitPrice) {
  if (entryPrice <= 0) return 0;
  return (exitPrice - entryPrice) / entryPrice;
}

function applyBuySlippage(price, config) {
  return Number(price) * (1 + normalizedRate(config.buySlippageRate));
}

function applySellSlippage(price, config) {
  return Number(price) * (1 - normalizedRate(config.sellSlippageRate));
}

function normalizedRate(value) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : 0;
}
