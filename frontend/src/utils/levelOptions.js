export function buildLevelOptions(form) {
  return {
    windowSize: form.windowSize,
    levelWindowSize: form.levelWindowSize,
    levelWindowStep: form.levelWindowStep,
    priceTolerance: form.bandRangePercent / 100,
    minTouches: form.minTouches,
    confirmBars: form.confirmBars,
    minWindowRange: form.minWindowRangePercent / 100,
    minLevelSpace: form.minLevelSpacePercent / 100,
    minRetestPullback: form.minRetestPullbackPercent / 100,
    minRetestSpanBars: form.minRetestSpanBars,
    retestLookbackBars: form.retestLookbackBars,
  };
}
