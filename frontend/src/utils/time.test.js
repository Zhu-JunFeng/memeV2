import { describe, expect, it } from 'vitest'

import { formatHoldingDuration } from './time.js'

describe('formatHoldingDuration', () => {
  it('formats an open position using the current time', () => {
    expect(formatHoldingDuration('2026-07-14T00:00:00Z', null, Date.parse('2026-07-14T00:02:21Z'))).toBe('2m21s')
  })

  it('formats a closed position using its close time', () => {
    expect(formatHoldingDuration('2026-07-14T00:00:00Z', '2026-07-14T00:00:34Z')).toBe('34s')
  })

  it('keeps longer durations compact', () => {
    expect(formatHoldingDuration('2026-07-14T00:00:00Z', '2026-07-14T01:02:03Z')).toBe('1h2m3s')
  })
})
