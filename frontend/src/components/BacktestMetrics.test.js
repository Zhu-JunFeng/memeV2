import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BacktestMetrics from './BacktestMetrics.vue'

describe('BacktestMetrics', () => {
  it('renders percent metrics', () => {
    const wrapper = mount(BacktestMetrics, { props: { metrics: { tradeCount: 2, totalProfitRate: 0.25, winRate: 0.5, maxDrawdownRate: 0.1, averageHoldingSeconds: 120 } } })
    expect(wrapper.text()).toContain('25.00%')
    expect(wrapper.text()).toContain('50.00%')
    expect(wrapper.text()).toContain('2 分钟')
  })
})
