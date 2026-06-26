<template>
  <div class="metrics-grid">
    <div v-for="item in items" :key="item.label" class="metric-card">
      <span>{{ item.label }}</span>
      <strong>{{ item.value }}</strong>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({ metrics: { type: Object, default: () => ({}) } })

function percent(value) {
  return `${((Number(value) || 0) * 100).toFixed(2)}%`
}

const items = computed(() => [
  { label: '交易次数', value: props.metrics.tradeCount ?? 0 },
  { label: '累计收益', value: percent(props.metrics.totalProfitRate) },
  { label: '胜率', value: percent(props.metrics.winRate) },
  { label: '最大回撤', value: percent(props.metrics.maxDrawdownRate) },
  { label: '平均持仓', value: `${Math.round((props.metrics.averageHoldingSeconds || 0) / 60)} 分钟` }
])
</script>
