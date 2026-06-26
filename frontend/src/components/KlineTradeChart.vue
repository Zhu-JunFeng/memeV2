<template>
  <div class="chart-shell">
    <div v-if="displayLevels.length" class="level-strip">
      <div>
        <strong>关键市值</strong>
        <span>仅显示强度最高的支撑/压力各 {{ LEVELS_PER_TYPE }} 条，隐藏 {{ hiddenLevelCount }} 条低权重线</span>
      </div>
      <div class="level-pills">
        <span
          v-for="level in displayLevels"
          :key="`${level.type}-${level.marketCap}`"
          class="level-pill"
          :class="level.type"
          :title="levelTooltip(level)"
        >
          {{ levelLabel(level) }}
          <em>{{ formatMarketCap(level.marketCap) }}</em>
        </span>
      </div>
    </div>
    <div ref="chartWrapEl" class="chart-wrap">
      <div ref="chartEl" class="chart-canvas"></div>
      <div v-if="hoveredTradeMarker" class="trade-hover-card" :style="hoverCardStyle">
        <div class="trade-hover-card__title">
          <span :class="['trade-hover-card__badge', hoveredTradeMarker.side]">{{ hoveredTradeMarker.side === 'buy' ? 'B' : 'S' }}</span>
          <strong>{{ hoveredTradeMarker.side === 'buy' ? '买点详情' : '卖点详情' }}</strong>
        </div>
        <div class="trade-hover-card__row">{{ formatTradeTime(hoveredTradeMarker.point.time) }}</div>
        <div class="trade-hover-card__row">{{ hoveredTradeMarker.side === 'buy' ? '买入市值' : '卖出市值' }} {{ formatMarketCap(hoveredTradeMarker.price) }}</div>
        <div class="trade-hover-card__row">买入市值 {{ formatMarketCap(hoveredTradeMarker.trade.buyPoint.marketCap || hoveredTradeMarker.trade.buyPoint.price) }}</div>
        <div class="trade-hover-card__row">窗口 W{{ hoveredTradeMarker.trade.windowIndex }} · {{ hoveredTradeMarker.trade.levelType === 'support' ? '支撑' : '压力' }}</div>
        <div class="trade-hover-card__row">{{ hoveredTradeMarker.side === 'buy' ? hoveredTradeMarker.trade.exitReason : `净收益 ${formatPercent(hoveredTradeMarker.trade.profitRate)}` }}</div>
      </div>
      <div
        class="selection-layer"
        @pointerdown="startSelection"
        @pointermove="handlePointerMove"
        @pointerup="finishSelection"
        @pointerleave="cancelSelection"
      >
        <div v-if="selectionRect" class="selection-rect" :style="selectionRectStyle"></div>
        <div v-if="!selectionRect" class="selection-hint">拖拽选择 K 线区域</div>
      </div>
    </div>
    <div v-if="!klines.length" class="chart-empty">等待 K 线数据</div>
  </div>
</template>

<script setup>
import { CandlestickSeries, createChart, createSeriesMarkers, LineStyle } from 'lightweight-charts'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { formatBeijingDateTime, formatChartTick, toUnixTimestamp } from '../utils/time.js'

const props = defineProps({
  klines: { type: Array, default: () => [] },
  levels: { type: Array, default: () => [] },
  selectedLevel: { type: Object, default: null },
  currentWindowIndex: { type: Number, default: 0 },
  windowColor: { type: String, default: '#60a5fa' },
  strategyTrades: { type: Array, default: () => [] },
  focusedTradeKey: { type: String, default: '' }
})
const emit = defineEmits(['range-selected'])

const LEVELS_PER_TYPE = 3
const chartEl = ref(null)
const chartWrapEl = ref(null)
let chart
let series
let markerApi
let resizeObserver
let priceLines = []
let hoverPriceLine = null
const isSelecting = ref(false)
const selectionStartX = ref(0)
const selectionCurrentX = ref(0)
const hoveredTradeMarker = ref(null)

const chartKlines = computed(() => {
  const byTime = new Map()
  props.klines.forEach((item) => {
    const time = toChartTime(item.openTime)
    if (!time || !validOHLC(item)) return
    byTime.set(time, {
      time,
      openTime: item.openTime,
      closeTime: item.closeTime,
      open: Number(item.marketCapOpen),
      high: Number(item.marketCapHigh),
      low: Number(item.marketCapLow),
      close: Number(item.marketCapClose)
    })
  })
  return [...byTime.values()].sort((left, right) => left.time - right.time)
})
const candles = computed(() => chartKlines.value.map((item) => ({
  time: item.time,
  open: item.open,
  high: item.high,
  low: item.low,
  close: item.close
})))

const displayLevels = computed(() => {
  const ranked = ['support', 'resistance'].flatMap((type) => props.levels
    .filter((level) => levelRole(level) === type && Number.isFinite(Number(level.marketCap)))
    .sort((left, right) => Number(right.score || 0) - Number(left.score || 0))
    .slice(0, LEVELS_PER_TYPE)
    .map((level, index) => ({ ...level, rank: index + 1 })))
  if (props.selectedLevel && !ranked.some((level) => isSameLevel(level, props.selectedLevel))) {
    ranked.push({ ...props.selectedLevel, rank: LEVELS_PER_TYPE + 1 })
  }
  return ranked.sort((left, right) => Number(left.marketCap) - Number(right.marketCap))
})

const hiddenLevelCount = computed(() => Math.max(0, props.levels.length - displayLevels.value.length))
const selectionRect = computed(() => {
  if (!isSelecting.value) return null
  const left = Math.min(selectionStartX.value, selectionCurrentX.value)
  const width = Math.abs(selectionCurrentX.value - selectionStartX.value)
  if (width < 2) return null
  return { left, width }
})
const selectionRectStyle = computed(() => selectionRect.value ? ({
  left: `${selectionRect.value.left}px`,
  width: `${selectionRect.value.width}px`
}) : {})
const hoverCardStyle = computed(() => {
  if (!hoveredTradeMarker.value) return {}
  const maxLeft = Math.max((chartEl.value?.clientWidth || 0) - 244, 12)
  return {
    left: `${Math.min(Math.max(hoveredTradeMarker.value.coordinateX + 16, 12), maxLeft)}px`,
    top: `${Math.max(hoveredTradeMarker.value.coordinateY - 20, 12)}px`
  }
})
const strategyMarkerDetails = computed(() => {
  if (!props.selectedLevel || !props.currentWindowIndex) return []
  const markers = []
  props.strategyTrades
    .filter((trade) => trade.windowIndex === props.currentWindowIndex && isTradeForSelectedLevel(trade))
    .forEach((trade) => {
    pushTradeMarker(markers, trade, 'buy', trade.buyPoint)
    pushTradeMarker(markers, trade, 'sell', trade.sellPoint)
  })
  return markers.sort((left, right) => left.time - right.time)
})

function toChartTime(value) {
  return toUnixTimestamp(value)
}

function validOHLC(item) {
  return ['marketCapOpen', 'marketCapHigh', 'marketCapLow', 'marketCapClose'].every((key) => Number.isFinite(Number(item[key])) && Number(item[key]) > 0)
}

function levelColor(level) {
  if (isSelectedLevel(level)) return '#f97316'
  const alpha = level.rank === 1 ? 0.75 : level.rank === 2 ? 0.5 : 0.28
  return withAlpha(levelRole(level) === 'support' ? '#22c55e' : '#f97316', alpha)
}

function isSelectedLevel(level) {
  return props.selectedLevel ? isSameLevel(level, props.selectedLevel) : false
}

function isSameLevel(left, right) {
  return left.type === right.type && Number(left.marketCap) === Number(right.marketCap)
}

function isTradeForSelectedLevel(trade) {
  const level = props.selectedLevel
  if (!level) return false
  return Number(trade.levelMarketCap) === Number(level.marketCap)
    && Number(trade.levelLowerMarketCap) === Number(level.lowerMarketCap)
    && Number(trade.levelUpperMarketCap) === Number(level.upperMarketCap)
}

function levelLabel(level) {
  return `${levelRole(level) === 'support' ? 'S' : 'R'}${level.rank}`
}

function levelTooltip(level) {
  const name = levelRole(level) === 'support' ? '支撑位' : '压力位'
  return `${name} · 市值 ${formatMarketCap(level.marketCap)} · 触碰 ${level.touches || 0} 次 · 强度 ${Number(level.score || 0).toFixed(1)}`
}

function levelRole(level) {
  if (Number(level?.calculation?.resistanceVotes || 0) > 0 && level?.breakout?.breakoutPoint) return 'resistance'
  return level?.type === 'support' ? 'support' : 'resistance'
}

function formatMarketCap(value) {
  const number = Number(value)
  if (!Number.isFinite(number)) return '-'
  if (Math.abs(number) >= 1_000_000) return `${trimDecimals(number / 1_000_000)}m`
  if (Math.abs(number) >= 1_000) return `${trimDecimals(number / 1_000)}k`
  return trimDecimals(number)
}

function trimDecimals(value) {
  return Number(value).toFixed(2).replace(/\.00$/, '').replace(/(\.\d)0$/, '$1')
}

function withAlpha(hex, alpha) {
  const normalized = hex.replace('#', '')
  if (normalized.length !== 6) return hex
  const red = Number.parseInt(normalized.slice(0, 2), 16)
  const green = Number.parseInt(normalized.slice(2, 4), 16)
  const blue = Number.parseInt(normalized.slice(4, 6), 16)
  return `rgba(${red}, ${green}, ${blue}, ${alpha})`
}

function renderChart() {
  if (!chartEl.value || !series) return
  series.setData(candles.value)
  renderPriceLines()
  renderMarkers()
  chart.timeScale().fitContent()
}

function renderPriceLines() {
  if (!series) return
  priceLines.forEach((line) => series.removePriceLine(line))
  priceLines = []
  if (hoverPriceLine) {
    series.removePriceLine(hoverPriceLine)
    hoverPriceLine = null
  }
  displayLevels.value.forEach((level) => {
    priceLines.push(series.createPriceLine({
      price: Number(level.marketCap),
      color: levelColor(level),
      lineWidth: isSelectedLevel(level) ? 2 : 1,
      lineStyle: isSelectedLevel(level) ? LineStyle.Solid : LineStyle.Dashed,
      axisLabelVisible: isSelectedLevel(level),
      title: isSelectedLevel(level) ? `${levelLabel(level)} 中心` : ''
    }))
  })
  renderSelectedLevelBandLines()
  renderHoveredTradeLine()
}

function renderSelectedLevelBandLines() {
  const level = props.selectedLevel
  if (!series || !level) return
  const lower = Number(level.lowerMarketCap)
  const upper = Number(level.upperMarketCap)
  if (!Number.isFinite(lower) || !Number.isFinite(upper) || lower <= 0 || upper <= 0) return
  const color = levelRole(level) === 'support' ? '#22c55e' : '#f97316'
  const lowerTitle = levelRole(level) === 'support' ? '支撑带下沿' : '压力带下沿'
  const upperTitle = levelRole(level) === 'support' ? '支撑带上沿' : '压力带上沿'
  priceLines.push(series.createPriceLine({
    price: lower,
    color: withAlpha(color, 0.55),
    lineWidth: 1,
    lineStyle: LineStyle.Dotted,
    axisLabelVisible: true,
    title: lowerTitle
  }))
  priceLines.push(series.createPriceLine({
    price: upper,
    color: withAlpha(color, 0.55),
    lineWidth: 1,
    lineStyle: LineStyle.Dotted,
    axisLabelVisible: true,
    title: upperTitle
  }))
}

function renderMarkers() {
  if (!markerApi) return
  markerApi.setMarkers([
    ...buildLevelMarkers(props.selectedLevel),
    ...buildTradeMarkers(strategyMarkerDetails.value)
  ].sort((left, right) => left.time - right.time))
}

function buildLevelMarkers(level) {
  if (!level) return []
  const markers = []
  const typeColor = levelRole(level) === 'support' ? '#22c55e' : '#f97316'
  const oppositeColor = levelRole(level) === 'support' ? '#86efac' : '#fdba74'
  const pivotPoints = level.calculation?.pivots || []
  const sampleTouches = level.calculation?.sampleTouches || []
  const failedTouches = level.breakout?.failedTouches || []
  pivotPoints.forEach((point, index) => {
    pushPointMarker(markers, point, {
      color: typeColor,
      shape: 'circle',
      text: `P${index + 1}`,
      position: 'atPriceTop',
      size: 0.8
    })
  })
  sampleTouches.forEach((point) => {
    pushPointMarker(markers, point, {
      color: withAlpha(oppositeColor, 0.8),
      shape: 'square',
      text: '触碰',
      position: 'atPriceMiddle',
      size: 0.65
    })
  })
  failedTouches.forEach((point) => {
    pushPointMarker(markers, point, {
      color: '#facc15',
      shape: 'arrowDown',
      text: '试压',
      position: 'atPriceTop',
      size: 0.85
    })
  })
  if (level.breakout?.breakoutPoint) {
    pushPointMarker(markers, level.breakout.breakoutPoint, {
      color: '#38bdf8',
      shape: 'arrowUp',
      text: '突破',
      position: 'atPriceTop',
      size: 1.15
    })
  }
  return markers.sort((left, right) => left.time - right.time)
}

function buildTradeMarkers(items) {
  return items.map((item) => ({
    time: item.time,
    price: item.price,
    position: item.side === 'buy' ? 'belowBar' : 'aboveBar',
    color: item.side === 'buy' ? (item.tradeKey === props.focusedTradeKey ? '#22c55e' : '#86efac') : (item.tradeKey === props.focusedTradeKey ? '#ef4444' : '#fca5a5'),
    shape: item.side === 'buy' ? 'arrowUp' : 'arrowDown',
    text: item.side === 'buy' ? 'B' : 'S',
    size: item.tradeKey === props.focusedTradeKey ? 1.3 : 1
  }))
}

function pushPointMarker(markers, point, options) {
  const time = nearestCandleTime(point?.time)
  const price = Number(point?.marketCap ?? point?.price)
  if (!time || !Number.isFinite(price) || price <= 0) return
  markers.push({
    time,
    price,
    position: options.position,
    color: options.color,
    shape: options.shape,
    text: options.text,
    size: options.size
  })
}

function pushTradeMarker(markers, trade, side, point) {
  const time = nearestCandleTime(point?.time)
  const price = Number(point?.marketCap ?? point?.price)
  if (!time || !Number.isFinite(price) || price <= 0) return
  markers.push({
    trade,
    tradeKey: tradeKey(trade),
    side,
    point,
    time,
    price
  })
}

function nearestCandleTime(value) {
  const target = toChartTime(value)
  if (!target || !candles.value.length) return null
  let bestTime = candles.value[0].time
  let bestDistance = Math.abs(bestTime - target)
  candles.value.forEach((item) => {
    const distance = Math.abs(item.time - target)
    if (distance < bestDistance) {
      bestDistance = distance
      bestTime = item.time
    }
  })
  return bestTime
}

function startSelection(event) {
  if (!chartWrapEl.value || !chartKlines.value.length) return
  event.preventDefault()
  event.currentTarget.setPointerCapture?.(event.pointerId)
  const x = pointerX(event)
  isSelecting.value = true
  selectionStartX.value = x
  selectionCurrentX.value = x
}

function moveSelection(event) {
  if (!isSelecting.value) return
  event.preventDefault()
  selectionCurrentX.value = pointerX(event)
  hoveredTradeMarker.value = null
  renderPriceLines()
}

function finishSelection(event) {
  if (!isSelecting.value) return
  event.preventDefault()
  event.currentTarget.releasePointerCapture?.(event.pointerId)
  selectionCurrentX.value = pointerX(event)
  const range = selectedKlineRange(selectionStartX.value, selectionCurrentX.value)
  isSelecting.value = false
  if (!range) return
  emit('range-selected', range)
}

function cancelSelection() {
  isSelecting.value = false
  hoveredTradeMarker.value = null
  renderPriceLines()
}

function pointerX(event) {
  const rect = chartEl.value?.getBoundingClientRect()
  if (!rect) return 0
  return Math.max(0, Math.min(rect.width, event.clientX - rect.left))
}

function pointerY(event) {
  const rect = chartEl.value?.getBoundingClientRect()
  if (!rect) return 0
  return Math.max(0, Math.min(rect.height, event.clientY - rect.top))
}

function selectedKlineRange(leftX, rightX) {
  if (!chart || !chartKlines.value.length || Math.abs(rightX - leftX) < 8) return null
  const first = nearestKlineByCoordinate(Math.min(leftX, rightX))
  const last = nearestKlineByCoordinate(Math.max(leftX, rightX))
  if (!first || !last) return null
  const startTime = first.time <= last.time ? first.openTime : last.openTime
  const endTime = first.time <= last.time ? last.closeTime : first.closeTime
  if (!startTime || !endTime || startTime === endTime) return null
  const selectedCount = chartKlines.value.filter((item) => item.time >= Math.min(first.time, last.time) && item.time <= Math.max(first.time, last.time)).length
  return { startTime, endTime, klineCount: selectedCount }
}

function nearestKlineByCoordinate(x) {
  const rawTime = chart.timeScale().coordinateToTime(x)
  const target = typeof rawTime === 'number' ? rawTime : toChartTime(rawTime)
  if (!target) return null
  let best = chartKlines.value[0]
  let bestDistance = Math.abs(best.time - target)
  chartKlines.value.forEach((item) => {
    const distance = Math.abs(item.time - target)
    if (distance < bestDistance) {
      best = item
      bestDistance = distance
    }
  })
  return best
}

function renderHoveredTradeLine() {
  if (!series || !hoveredTradeMarker.value) return
  hoverPriceLine = series.createPriceLine({
    price: hoveredTradeMarker.value.price,
    color: hoveredTradeMarker.value.side === 'buy' ? 'rgba(34, 197, 94, 0.82)' : 'rgba(239, 68, 68, 0.82)',
    lineWidth: 1,
    lineStyle: LineStyle.Dashed,
    axisLabelVisible: true,
    title: hoveredTradeMarker.value.side === 'buy' ? 'B 买点价' : 'S 卖点价'
  })
}

function handlePointerMove(event) {
  if (isSelecting.value) {
    moveSelection(event)
    return
  }
  updateHoveredTradeMarker(pointerX(event), pointerY(event))
}

function updateHoveredTradeMarker(x, y) {
  const nextHovered = findHoveredTradeMarker(x, y)
  const currentKey = hoveredTradeMarker.value?.markerKey || ''
  const nextKey = nextHovered?.markerKey || ''
  if (currentKey === nextKey) return
  hoveredTradeMarker.value = nextHovered
  renderPriceLines()
}

function findHoveredTradeMarker(x, y) {
  if (!chart || !series || !strategyMarkerDetails.value.length) return null
  let best = null
  let bestDistance = Number.POSITIVE_INFINITY
  strategyMarkerDetails.value.forEach((item) => {
    const coordinateX = chart.timeScale().timeToCoordinate(item.time)
    const coordinateY = series.priceToCoordinate(item.price)
    if (!Number.isFinite(coordinateX) || !Number.isFinite(coordinateY)) return
    const dx = coordinateX - x
    const dy = coordinateY - y
    const distance = Math.sqrt(dx * dx + dy * dy)
    if (distance <= 16 && distance < bestDistance) {
      bestDistance = distance
      best = {
        ...item,
        coordinateX,
        coordinateY,
        markerKey: `${item.tradeKey}-${item.side}`
      }
    }
  })
  return best
}

function tradeKey(trade) {
  return `${trade.takeProfitRate}-${trade.windowIndex}-${trade.levelIndex}-${trade.buyPoint?.time}-${trade.sellPoint?.time}`
}

function formatTradeTime(value) {
  return formatBeijingDateTime(value)
}

function formatPercent(value) {
  const number = Number(value)
  return Number.isFinite(number) ? `${(number * 100).toFixed(2).replace(/\.00$/, '').replace(/(\.\d)0$/, '$1')}%` : '-'
}

onMounted(() => {
  if (!chartEl.value) return
  chart = createChart(chartEl.value, {
    width: chartEl.value.clientWidth,
    height: 520,
    attributionLogo: false,
    layout: { background: { color: 'transparent' }, textColor: '#d8e9e2', fontFamily: 'Fira Sans, Avenir Next, sans-serif' },
    grid: { vertLines: { color: 'rgba(143,178,168,0.08)' }, horzLines: { color: 'rgba(143,178,168,0.12)' } },
    localization: {
      locale: 'zh-CN',
      priceFormatter: (value) => formatMarketCap(value)
    },
    crosshair: { mode: 0 },
    rightPriceScale: { borderColor: '#2b4a4d', scaleMargins: { top: 0.08, bottom: 0.12 } },
    timeScale: {
      borderColor: '#2b4a4d',
      timeVisible: true,
      secondsVisible: false,
      tickMarkFormatter: (value) => formatChartTick(value)
    }
  })
  series = chart.addSeries(CandlestickSeries, {
    upColor: '#14b86a',
    downColor: '#ef4444',
    wickUpColor: '#14b86a',
    wickDownColor: '#ef4444',
    borderVisible: false,
    priceFormat: { type: 'price', precision: 2, minMove: 0.01 }
  })
  markerApi = createSeriesMarkers(series, [], { zOrder: 'top' })
  resizeObserver = new ResizeObserver(() => {
    chart?.applyOptions({
      width: chartEl.value?.clientWidth || 0,
      height: chartEl.value?.clientHeight || 520
    })
  })
  resizeObserver.observe(chartWrapEl.value || chartEl.value)
  renderChart()
})

watch(() => [props.klines, props.levels, props.selectedLevel, props.strategyTrades, props.focusedTradeKey], () => {
  hoveredTradeMarker.value = null
  renderChart()
}, { deep: true })

onBeforeUnmount(() => {
  priceLines.forEach((line) => series?.removePriceLine(line))
  priceLines = []
  if (hoverPriceLine) {
    series?.removePriceLine(hoverPriceLine)
    hoverPriceLine = null
  }
  markerApi?.detach()
  markerApi = null
  resizeObserver?.disconnect()
  chart?.remove()
})
</script>

<style scoped>
.chart-shell {
  position: relative;
}

.chart-wrap {
  position: relative;
}

.chart-canvas {
  min-height: 520px;
}

.trade-hover-card {
  position: absolute;
  z-index: 4;
  width: 228px;
  padding: 10px 12px;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 12px;
  background: rgba(7, 16, 19, 0.94);
  color: #e5f3ee;
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.28);
  pointer-events: none;
}

.trade-hover-card__title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.trade-hover-card__badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 999px;
  color: #071013;
  font-size: 12px;
  font-weight: 800;
}

.trade-hover-card__badge.buy {
  background: #86efac;
}

.trade-hover-card__badge.sell {
  background: #fca5a5;
}

.trade-hover-card__title strong {
  font-size: 13px;
}

.trade-hover-card__row {
  color: #b8ccc4;
  font-size: 12px;
  line-height: 1.5;
}

.selection-layer {
  position: absolute;
  inset: 0;
  cursor: crosshair;
  touch-action: none;
  user-select: none;
}

.selection-rect {
  position: absolute;
  top: 0;
  bottom: 0;
  border-left: 1px solid rgba(56, 189, 248, 0.9);
  border-right: 1px solid rgba(56, 189, 248, 0.9);
  background: linear-gradient(180deg, rgba(56, 189, 248, 0.18), rgba(14, 165, 233, 0.06));
  box-shadow: inset 0 0 0 1px rgba(56, 189, 248, 0.16);
}

.selection-hint {
  position: absolute;
  top: 12px;
  right: 12px;
  padding: 6px 10px;
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.58);
  color: rgba(216, 233, 226, 0.72);
  font-size: 12px;
  pointer-events: none;
}
</style>
