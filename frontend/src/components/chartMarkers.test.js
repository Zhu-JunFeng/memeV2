import { describe, expect, it } from 'vitest'
import { toChartSeries, toTradeMarkers } from './chartMarkers.js'
import { formatBeijingDateTime } from '../utils/time.js'

describe('chartMarkers', () => {
  it('converts klines to lightweight chart candles', () => {
    const candles = toChartSeries([{ openTime: '2026-06-22T00:00:00Z', open: '1', high: '2', low: '0.5', close: '1.5' }])
    expect(candles[0]).toMatchObject({ open: 1, high: 2, low: 0.5, close: 1.5 })
  })

  it('converts trade points to buy and sell markers', () => {
    const markers = toTradeMarkers([
      { side: 'buy', matchedKlineTime: '2026-06-22T00:00:00Z' },
      { side: 'sell', matchedKlineTime: '2026-06-22T00:01:00Z' }
    ])
    expect(markers[0].position).toBe('belowBar')
    expect(markers[1].position).toBe('aboveBar')
    expect(markers[0].text).toBe('B')
    expect(markers[1].text).toBe('S')
  })

  it('formats timestamps in Beijing time', () => {
    expect(formatBeijingDateTime('2026-06-22T00:00:00Z')).toBe('2026-06-22 08:00:00')
  })
})
