import { toUnixTimestamp } from '../utils/time.js'

export function toChartSeries(klines = []) {
  return klines.map((item) => ({
    time: toUnixTimestamp(item.openTime),
    open: Number(item.open),
    high: Number(item.high),
    low: Number(item.low),
    close: Number(item.close)
  }))
}

export function toTradeMarkers(points = []) {
  return points.map((point) => ({
    time: toUnixTimestamp(point.matchedKlineTime || point.time),
    position: point.side === 'buy' ? 'belowBar' : 'aboveBar',
    color: point.side === 'buy' ? '#14b86a' : '#ef4444',
    shape: point.side === 'buy' ? 'arrowUp' : 'arrowDown',
    text: point.side === 'buy' ? 'B' : 'S'
  }))
}
