<template>
  <section class="panel">
    <div class="panel-title">逐笔结果</div>
    <el-table :data="trades" size="small">
      <el-table-column label="买入时间" min-width="180">
        <template #default="{ row }">{{ formatBeijingDateTime(row.buy?.time) }}</template>
      </el-table-column>
      <el-table-column label="卖出时间" min-width="180">
        <template #default="{ row }">{{ formatBeijingDateTime(row.sell?.time) }}</template>
      </el-table-column>
      <el-table-column label="买入价">
        <template #default="{ row }">{{ formatPrice(row.buy?.matchedPrice) }}</template>
      </el-table-column>
      <el-table-column label="卖出价">
        <template #default="{ row }">{{ formatPrice(row.sell?.matchedPrice) }}</template>
      </el-table-column>
      <el-table-column label="收益率">
        <template #default="{ row }">
          <span :class="row.profitRate >= 0 ? 'profit' : 'loss'">{{ formatPercent(row.profitRate) }}</span>
        </template>
      </el-table-column>
    </el-table>
  </section>
</template>

<script setup>
import { formatBeijingDateTime } from '../utils/time.js'

defineProps({ trades: { type: Array, default: () => [] } })

function formatPrice(value) {
  return Number(value || 0).toPrecision(6)
}

function formatPercent(value) {
  return `${(Number(value || 0) * 100).toFixed(2)}%`
}
</script>
