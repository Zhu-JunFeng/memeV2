<template>
  <div class="chart-shell">
    <div class="chart-toolbar">
      <div class="chart-toolbar__group">
        <span class="chart-toolbar__label">指标</span>
        <button
          v-for="option in indicatorOptions"
          :key="option.key"
          type="button"
          class="toolbar-chip"
          :class="{ active: enabledIndicators.includes(option.key) }"
          @click="toggleIndicator(option.key)"
        >
          {{ option.label }}
        </button>
      </div>
      <div class="chart-toolbar__group">
        <span class="chart-toolbar__label">画线</span>
        <button
          v-for="tool in toolOptions"
          :key="tool.key"
          type="button"
          class="toolbar-chip"
          :class="{ active: activeTool === tool.key }"
          @click="setActiveTool(tool.key)"
        >
          {{ tool.label }}
        </button>
        <button
          type="button"
          class="toolbar-chip danger"
          :disabled="!drawings.length"
          @click="clearDrawings"
        >
          清空画线
        </button>
      </div>
    </div>

    <div class="chart-status-bar">
      <div v-if="displayLevels.length" class="level-strip">
        <div>
          <strong>关键价位</strong>
          <span
            >仅显示强度最高的支撑/压力各 {{ LEVELS_PER_TYPE }} 条，隐藏
            {{ hiddenLevelCount }} 条低权重线</span
          >
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
      <div class="chart-hint-group">
        <span v-if="activeTool === 'range'">拖拽选择 K 线区域</span>
        <span v-else-if="activeTool === 'horizontal'">单击图表添加水平线</span>
        <span v-else-if="activeTool === 'trend' && pendingTrendPoint"
          >已选起点，再点一次完成趋势线</span
        >
        <span v-else-if="activeTool === 'trend'">单击两次完成趋势线</span>
        <span v-if="drawings.length">已添加 {{ drawings.length }} 条画线</span>
      </div>
    </div>

    <div ref="chartWrapEl" class="chart-wrap">
      <div ref="chartEl" class="chart-canvas"></div>
      <svg
        v-if="visibleDrawings.length"
        class="drawing-overlay"
        :width="overlaySize.width"
        :height="overlaySize.height"
      >
        <g v-for="drawing in visibleDrawings" :key="drawing.id">
          <line
            :x1="drawing.x1"
            :y1="drawing.y1"
            :x2="drawing.x2"
            :y2="drawing.y2"
            :stroke="drawing.color"
            :stroke-dasharray="drawing.type === 'horizontal' ? '6 4' : '0'"
            stroke-width="2"
            stroke-linecap="round"
          />
          <circle
            v-if="drawing.type === 'trend'"
            :cx="drawing.x1"
            :cy="drawing.y1"
            r="4"
            :fill="drawing.color"
          />
          <circle
            v-if="drawing.type === 'trend'"
            :cx="drawing.x2"
            :cy="drawing.y2"
            r="4"
            :fill="drawing.color"
          />
          <text
            :x="drawing.labelX"
            :y="drawing.labelY"
            :fill="drawing.color"
            class="drawing-label"
          >
            {{ drawing.label }}
          </text>
        </g>
      </svg>
      <div
        v-if="hoveredTradeMarker"
        class="trade-hover-card"
        :style="hoverCardStyle"
      >
        <div class="trade-hover-card__title">
          <span :class="['trade-hover-card__badge', hoveredTradeMarker.side]">{{
            hoveredTradeMarker.side === "buy" ? "B" : "S"
          }}</span>
          <strong>{{
            hoveredTradeMarker.side === "buy" ? "买点详情" : "卖点详情"
          }}</strong>
        </div>
        <div class="trade-hover-card__row">
          {{ formatTradeTime(hoveredTradeMarker.point.time) }}
        </div>
        <div class="trade-hover-card__row">
          {{ hoveredTradeMarker.side === "buy" ? "买入价位" : "卖出价位" }}
          {{ formatMarketCap(hoveredTradeMarker.price) }}
        </div>
        <div class="trade-hover-card__row">
          买入价位
          {{
            formatMarketCap(
              hoveredTradeMarker.trade.buyPoint.marketCap ||
                hoveredTradeMarker.trade.buyPoint.price,
            )
          }}
        </div>
        <div class="trade-hover-card__row">
          窗口 W{{ hoveredTradeMarker.trade.windowIndex }} ·
          {{
            hoveredTradeMarker.trade.levelType === "support" ? "支撑" : "压力"
          }}
        </div>
        <div class="trade-hover-card__row">
          {{
            hoveredTradeMarker.side === "buy"
              ? hoveredTradeMarker.trade.exitReason
              : `净收益 ${formatPercent(hoveredTradeMarker.trade.profitRate)}`
          }}
        </div>
      </div>
      <div
        class="selection-layer"
        :class="`tool-${activeTool}`"
        @pointerdown="handlePointerDown"
        @pointermove="handlePointerMove"
        @pointerup="finishSelection"
        @pointerleave="cancelSelection"
      >
        <div
          v-if="selectionRect"
          class="selection-rect"
          :style="selectionRectStyle"
        ></div>
        <div
          v-if="!selectionRect && activeTool === 'range'"
          class="selection-hint"
        >
          拖拽选择 K 线区域
        </div>
      </div>
    </div>
    <div v-if="!klines.length" class="chart-empty">等待 K 线数据</div>
  </div>
</template>

<script setup>
import {
  CandlestickSeries,
  HistogramSeries,
  LineSeries,
  createChart,
  createSeriesMarkers,
  LineStyle,
} from "lightweight-charts";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  formatBeijingDateTime,
  formatChartCrosshairTime,
  formatChartTick,
  toUnixTimestamp,
} from "../utils/time.js";

const props = defineProps({
  klines: { type: Array, default: () => [] },
  levels: { type: Array, default: () => [] },
  selectedLevel: { type: Object, default: null },
  currentWindowIndex: { type: Number, default: 0 },
  windowColor: { type: String, default: "#60a5fa" },
  strategyTrades: { type: Array, default: () => [] },
  focusedTradeKey: { type: String, default: "" },
});
const emit = defineEmits(["range-selected"]);

const LEVELS_PER_TYPE = 3;
const indicatorOptions = [
  { key: "vol", label: "VOL" },
  { key: "ma7", label: "MA7" },
  { key: "ma20", label: "MA20" },
  { key: "ma60", label: "MA60" },
  { key: "ema20", label: "EMA20" },
  { key: "boll", label: "BOLL" },
];
const toolOptions = [
  { key: "range", label: "区域" },
  { key: "horizontal", label: "水平线" },
  { key: "trend", label: "趋势线" },
];
const indicatorColors = {
  ma7: "#facc15",
  ma20: "#38bdf8",
  ma60: "#c084fc",
  ema20: "#fb7185",
  bollUpper: "#f97316",
  bollMiddle: "#94a3b8",
  bollLower: "#22c55e",
};
const drawingColors = {
  horizontal: "#f59e0b",
  trend: "#38bdf8",
};

const chartEl = ref(null);
const chartWrapEl = ref(null);
let chart;
let series;
let markerApi;
let resizeObserver;
let priceLines = [];
let hoverPriceLine = null;
let volumeSeries = null;
const indicatorSeries = new Map();
const enabledIndicators = ref([]);
const activeTool = ref("range");
const drawings = ref([]);
const pendingTrendPoint = ref(null);
const isSelecting = ref(false);
const selectionStartX = ref(0);
const selectionCurrentX = ref(0);
const hoveredTradeMarker = ref(null);
const overlayRevision = ref(0);

const chartKlines = computed(() => {
  const byTime = new Map();
  props.klines.forEach((item) => {
    const time = toChartTime(item.openTime);
    if (!time || !validOHLC(item)) return;
    byTime.set(time, {
      time,
      openTime: item.openTime,
      closeTime: item.closeTime,
      open: Number(item.marketCapOpen),
      high: Number(item.marketCapHigh),
      low: Number(item.marketCapLow),
      close: Number(item.marketCapClose),
      volume: Number(item.volume || 0),
    });
  });
  return [...byTime.values()].sort((left, right) => left.time - right.time);
});
const candles = computed(() =>
  chartKlines.value.map((item) => ({
    time: item.time,
    open: item.open,
    high: item.high,
    low: item.low,
    close: item.close,
  })),
);
const overlaySize = computed(() => ({
  width: chartEl.value?.clientWidth || 0,
  height: chartEl.value?.clientHeight || 520,
}));

const displayLevels = computed(() => {
  const ranked = ["support", "resistance"].flatMap((type) =>
    props.levels
      .filter(
        (level) =>
          levelRole(level) === type && Number.isFinite(Number(level.marketCap)),
      )
      .sort((left, right) => Number(right.score || 0) - Number(left.score || 0))
      .slice(0, LEVELS_PER_TYPE)
      .map((level, index) => ({ ...level, rank: index + 1 })),
  );
  if (
    props.selectedLevel &&
    !ranked.some((level) => isSameLevel(level, props.selectedLevel))
  ) {
    ranked.push({ ...props.selectedLevel, rank: LEVELS_PER_TYPE + 1 });
  }
  return ranked.sort(
    (left, right) => Number(left.marketCap) - Number(right.marketCap),
  );
});

const hiddenLevelCount = computed(() =>
  Math.max(0, props.levels.length - displayLevels.value.length),
);
const selectionRect = computed(() => {
  if (!isSelecting.value) return null;
  const left = Math.min(selectionStartX.value, selectionCurrentX.value);
  const width = Math.abs(selectionCurrentX.value - selectionStartX.value);
  if (width < 2) return null;
  return { left, width };
});
const selectionRectStyle = computed(() =>
  selectionRect.value
    ? {
        left: `${selectionRect.value.left}px`,
        width: `${selectionRect.value.width}px`,
      }
    : {},
);
const hoverCardStyle = computed(() => {
  if (!hoveredTradeMarker.value) return {};
  const maxLeft = Math.max((chartEl.value?.clientWidth || 0) - 244, 12);
  return {
    left: `${Math.min(Math.max(hoveredTradeMarker.value.coordinateX + 16, 12), maxLeft)}px`,
    top: `${Math.max(hoveredTradeMarker.value.coordinateY - 20, 12)}px`,
  };
});
const strategyMarkerDetails = computed(() => {
  if (!props.selectedLevel || !props.currentWindowIndex) return [];
  const markers = [];
  props.strategyTrades
    .filter(
      (trade) =>
        trade.windowIndex === props.currentWindowIndex &&
        isTradeForSelectedLevel(trade),
    )
    .forEach((trade) => {
      pushTradeMarker(markers, trade, "buy", trade.buyPoint);
      pushTradeMarker(markers, trade, "sell", trade.sellPoint);
    });
  return markers.sort((left, right) => left.time - right.time);
});
const visibleDrawings = computed(() => {
  overlayRevision.value;
  if (
    !chart ||
    !series ||
    !overlaySize.value.width ||
    !overlaySize.value.height
  )
    return [];
  return drawings.value
    .map((drawing) => mapDrawingToCoordinates(drawing))
    .filter(Boolean);
});

function toChartTime(value) {
  return toUnixTimestamp(value);
}

function validOHLC(item) {
  return [
    "marketCapOpen",
    "marketCapHigh",
    "marketCapLow",
    "marketCapClose",
  ].every((key) => Number.isFinite(Number(item[key])) && Number(item[key]) > 0);
}

function levelColor(level) {
  if (isSelectedLevel(level)) return "#f97316";
  const alpha = level.rank === 1 ? 0.75 : level.rank === 2 ? 0.5 : 0.28;
  return withAlpha(
    levelRole(level) === "support" ? "#22c55e" : "#f97316",
    alpha,
  );
}

function isSelectedLevel(level) {
  return props.selectedLevel ? isSameLevel(level, props.selectedLevel) : false;
}

function isSameLevel(left, right) {
  return (
    left.type === right.type &&
    Number(left.marketCap) === Number(right.marketCap)
  );
}

function isTradeForSelectedLevel(trade) {
  const level = props.selectedLevel;
  if (!level) return false;
  return (
    Number(trade.levelMarketCap) === Number(level.marketCap) &&
    Number(trade.levelLowerMarketCap) === Number(level.lowerMarketCap) &&
    Number(trade.levelUpperMarketCap) === Number(level.upperMarketCap)
  );
}

function levelLabel(level) {
  return `${levelRole(level) === "support" ? "S" : "R"}${level.rank}`;
}

function levelTooltip(level) {
  const name = levelRole(level) === "support" ? "支撑位" : "压力位";
  return `${name} · 价位 ${formatMarketCap(level.marketCap)} · 触碰 ${level.touches || 0} 次 · 强度 ${Number(level.score || 0).toFixed(1)}`;
}

function levelRole(level) {
  if (
    Number(level?.calculation?.resistanceVotes || 0) > 0 &&
    level?.breakout?.breakoutPoint
  )
    return "resistance";
  return level?.type === "support" ? "support" : "resistance";
}

function formatMarketCap(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  if (Math.abs(number) >= 1_000_000)
    return `${trimDecimals(number / 1_000_000)}m`;
  if (Math.abs(number) >= 1_000) return `${trimDecimals(number / 1_000)}k`;
  return trimDecimals(number);
}

function trimDecimals(value) {
  return Number(value)
    .toFixed(2)
    .replace(/\.00$/, "")
    .replace(/(\.\d)0$/, "$1");
}

function withAlpha(hex, alpha) {
  const normalized = hex.replace("#", "");
  if (normalized.length !== 6) return hex;
  const red = Number.parseInt(normalized.slice(0, 2), 16);
  const green = Number.parseInt(normalized.slice(2, 4), 16);
  const blue = Number.parseInt(normalized.slice(4, 6), 16);
  return `rgba(${red}, ${green}, ${blue}, ${alpha})`;
}

function renderChart() {
  if (!chartEl.value || !series) return;
  series.setData(candles.value);
  syncPriceScaleMargins();
  syncIndicatorSeries();
  renderPriceLines();
  renderMarkers();
  chart.timeScale().fitContent();
  touchOverlay();
}

function syncPriceScaleMargins() {
  chart?.priceScale("right")?.applyOptions({
    borderColor: "#2b4a4d",
    scaleMargins: enabledIndicators.value.includes("vol")
      ? { top: 0.08, bottom: 0.26 }
      : { top: 0.08, bottom: 0.12 },
  });
}

function renderPriceLines() {
  if (!series) return;
  priceLines.forEach((line) => series.removePriceLine(line));
  priceLines = [];
  if (hoverPriceLine) {
    series.removePriceLine(hoverPriceLine);
    hoverPriceLine = null;
  }
  displayLevels.value.forEach((level) => {
    priceLines.push(
      series.createPriceLine({
        price: Number(level.marketCap),
        color: levelColor(level),
        lineWidth: isSelectedLevel(level) ? 2 : 1,
        lineStyle: isSelectedLevel(level) ? LineStyle.Solid : LineStyle.Dashed,
        axisLabelVisible: isSelectedLevel(level),
        title: isSelectedLevel(level) ? `${levelLabel(level)} 中心` : "",
      }),
    );
  });
  drawings.value
    .filter((item) => item.type === "horizontal")
    .forEach((item, index) => {
      priceLines.push(
        series.createPriceLine({
          price: item.price,
          color: drawingColors.horizontal,
          lineWidth: 1,
          lineStyle: LineStyle.Dashed,
          axisLabelVisible: true,
          title: `H${index + 1}`,
        }),
      );
    });
  renderSelectedLevelBandLines();
  renderHoveredTradeLine();
}

function renderSelectedLevelBandLines() {
  const level = props.selectedLevel;
  if (!series || !level) return;
  const lower = Number(level.lowerMarketCap);
  const upper = Number(level.upperMarketCap);
  if (
    !Number.isFinite(lower) ||
    !Number.isFinite(upper) ||
    lower <= 0 ||
    upper <= 0
  )
    return;
  const color = levelRole(level) === "support" ? "#22c55e" : "#f97316";
  const lowerTitle =
    levelRole(level) === "support" ? "支撑带下沿" : "压力带下沿";
  const upperTitle =
    levelRole(level) === "support" ? "支撑带上沿" : "压力带上沿";
  priceLines.push(
    series.createPriceLine({
      price: lower,
      color: withAlpha(color, 0.55),
      lineWidth: 1,
      lineStyle: LineStyle.Dotted,
      axisLabelVisible: true,
      title: lowerTitle,
    }),
  );
  priceLines.push(
    series.createPriceLine({
      price: upper,
      color: withAlpha(color, 0.55),
      lineWidth: 1,
      lineStyle: LineStyle.Dotted,
      axisLabelVisible: true,
      title: upperTitle,
    }),
  );
}

function renderMarkers() {
  if (!markerApi) return;
  markerApi.setMarkers(
    [
      ...buildLevelMarkers(props.selectedLevel),
      ...buildTradeMarkers(strategyMarkerDetails.value),
    ].sort((left, right) => left.time - right.time),
  );
}

function buildLevelMarkers(level) {
  if (!level) return [];
  const markers = [];
  const sampleTouches = level.calculation?.sampleTouches || [];
  const failedTouches = level.breakout?.failedTouches || [];
  const displayTouches = failedTouches.length ? failedTouches : sampleTouches.slice(0, 3);
  displayTouches.forEach((point, index) => {
    pushPointMarker(markers, point, {
      color: "#facc15",
      shape: "arrowDown",
      text: `试压${index + 1}`,
      position: "atPriceTop",
      size: 0.85,
    });
  });
  if (level.breakout?.breakoutPoint) {
    pushPointMarker(markers, level.breakout.breakoutPoint, {
      color: "#38bdf8",
      shape: "arrowUp",
      text: "突破",
      position: "atPriceTop",
      size: 1.15,
    });
  }
  return markers.sort((left, right) => left.time - right.time);
}

function buildTradeMarkers(items) {
  return items.map((item) => ({
    time: item.time,
    price: item.price,
    position: item.side === "buy" ? "belowBar" : "aboveBar",
    color:
      item.side === "buy"
        ? item.tradeKey === props.focusedTradeKey
          ? "#22c55e"
          : "#86efac"
        : item.tradeKey === props.focusedTradeKey
          ? "#ef4444"
          : "#fca5a5",
    shape: item.side === "buy" ? "arrowUp" : "arrowDown",
    text: item.side === "buy" ? "B" : "S",
    size: item.tradeKey === props.focusedTradeKey ? 1.3 : 1,
  }));
}

function pushPointMarker(markers, point, options) {
  const time = nearestCandleTime(point?.time);
  const price = Number(point?.marketCap ?? point?.price);
  if (!time || !Number.isFinite(price) || price <= 0) return;
  markers.push({
    time,
    price,
    position: options.position,
    color: options.color,
    shape: options.shape,
    text: options.text,
    size: options.size,
  });
}

function pushTradeMarker(markers, trade, side, point) {
  const time = nearestCandleTime(point?.time);
  const price = Number(point?.marketCap ?? point?.price);
  if (!time || !Number.isFinite(price) || price <= 0) return;
  markers.push({
    trade,
    tradeKey: tradeKey(trade),
    side,
    point,
    time,
    price,
  });
}

function nearestCandleTime(value) {
  const target = toChartTime(value);
  if (!target || !candles.value.length) return null;
  let bestTime = candles.value[0].time;
  let bestDistance = Math.abs(bestTime - target);
  candles.value.forEach((item) => {
    const distance = Math.abs(item.time - target);
    if (distance < bestDistance) {
      bestDistance = distance;
      bestTime = item.time;
    }
  });
  return bestTime;
}

function handlePointerDown(event) {
  if (activeTool.value === "range") {
    startSelection(event);
    return;
  }
  addDrawingFromPointer(event);
}

function startSelection(event) {
  if (!chartWrapEl.value || !chartKlines.value.length) return;
  event.preventDefault();
  event.currentTarget.setPointerCapture?.(event.pointerId);
  const x = pointerX(event);
  isSelecting.value = true;
  selectionStartX.value = x;
  selectionCurrentX.value = x;
}

function moveSelection(event) {
  if (!isSelecting.value) return;
  event.preventDefault();
  selectionCurrentX.value = pointerX(event);
  hoveredTradeMarker.value = null;
  renderPriceLines();
}

function finishSelection(event) {
  if (!isSelecting.value) return;
  event.preventDefault();
  event.currentTarget.releasePointerCapture?.(event.pointerId);
  selectionCurrentX.value = pointerX(event);
  const range = selectedKlineRange(
    selectionStartX.value,
    selectionCurrentX.value,
  );
  isSelecting.value = false;
  if (!range) return;
  emit("range-selected", range);
}

function cancelSelection() {
  isSelecting.value = false;
  hoveredTradeMarker.value = null;
  renderPriceLines();
}

function addDrawingFromPointer(event) {
  if (!chart || !series || !chartKlines.value.length) return;
  event.preventDefault();
  const point = pointerToDrawingPoint(event);
  if (!point) return;
  if (activeTool.value === "horizontal") {
    drawings.value = [
      ...drawings.value,
      {
        id: `horizontal-${Date.now()}-${drawings.value.length}`,
        type: "horizontal",
        price: point.price,
      },
    ];
    renderPriceLines();
    touchOverlay();
    return;
  }
  if (activeTool.value === "trend") {
    if (!pendingTrendPoint.value) {
      pendingTrendPoint.value = point;
      touchOverlay();
      return;
    }
    drawings.value = [
      ...drawings.value,
      {
        id: `trend-${Date.now()}-${drawings.value.length}`,
        type: "trend",
        start: pendingTrendPoint.value,
        end: point,
      },
    ];
    pendingTrendPoint.value = null;
    touchOverlay();
  }
}

function pointerX(event) {
  const rect = chartEl.value?.getBoundingClientRect();
  if (!rect) return 0;
  return Math.max(0, Math.min(rect.width, event.clientX - rect.left));
}

function pointerY(event) {
  const rect = chartEl.value?.getBoundingClientRect();
  if (!rect) return 0;
  return Math.max(0, Math.min(rect.height, event.clientY - rect.top));
}

function pointerToDrawingPoint(event) {
  const x = pointerX(event);
  const y = pointerY(event);
  const candle = nearestKlineByCoordinate(x);
  const price = series.coordinateToPrice(y);
  if (!candle || !Number.isFinite(price) || price <= 0) return null;
  return {
    time: candle.time,
    price,
  };
}

function selectedKlineRange(leftX, rightX) {
  if (!chart || !chartKlines.value.length || Math.abs(rightX - leftX) < 8)
    return null;
  const first = nearestKlineByCoordinate(Math.min(leftX, rightX));
  const last = nearestKlineByCoordinate(Math.max(leftX, rightX));
  if (!first || !last) return null;
  const startTime = first.time <= last.time ? first.openTime : last.openTime;
  const endTime = first.time <= last.time ? last.closeTime : first.closeTime;
  if (!startTime || !endTime || startTime === endTime) return null;
  const selectedCount = chartKlines.value.filter(
    (item) =>
      item.time >= Math.min(first.time, last.time) &&
      item.time <= Math.max(first.time, last.time),
  ).length;
  return { startTime, endTime, klineCount: selectedCount };
}

function nearestKlineByCoordinate(x) {
  const rawTime = chart.timeScale().coordinateToTime(x);
  const target = typeof rawTime === "number" ? rawTime : toChartTime(rawTime);
  if (!target) return null;
  let best = chartKlines.value[0];
  let bestDistance = Math.abs(best.time - target);
  chartKlines.value.forEach((item) => {
    const distance = Math.abs(item.time - target);
    if (distance < bestDistance) {
      best = item;
      bestDistance = distance;
    }
  });
  return best;
}

function renderHoveredTradeLine() {
  if (!series || !hoveredTradeMarker.value) return;
  hoverPriceLine = series.createPriceLine({
    price: hoveredTradeMarker.value.price,
    color:
      hoveredTradeMarker.value.side === "buy"
        ? "rgba(34, 197, 94, 0.82)"
        : "rgba(239, 68, 68, 0.82)",
    lineWidth: 1,
    lineStyle: LineStyle.Dashed,
    axisLabelVisible: true,
    title: hoveredTradeMarker.value.side === "buy" ? "B 买点价" : "S 卖点价",
  });
}

function handlePointerMove(event) {
  if (isSelecting.value) {
    moveSelection(event);
    touchOverlay();
    return;
  }
  updateHoveredTradeMarker(pointerX(event), pointerY(event));
}

function updateHoveredTradeMarker(x, y) {
  const nextHovered = findHoveredTradeMarker(x, y);
  const currentKey = hoveredTradeMarker.value?.markerKey || "";
  const nextKey = nextHovered?.markerKey || "";
  if (currentKey === nextKey) return;
  hoveredTradeMarker.value = nextHovered;
  renderPriceLines();
}

function findHoveredTradeMarker(x, y) {
  if (
    !chart ||
    !series ||
    !strategyMarkerDetails.value.length ||
    activeTool.value !== "range"
  )
    return null;
  let best = null;
  let bestDistance = Number.POSITIVE_INFINITY;
  strategyMarkerDetails.value.forEach((item) => {
    const coordinateX = chart.timeScale().timeToCoordinate(item.time);
    const coordinateY = series.priceToCoordinate(item.price);
    if (!Number.isFinite(coordinateX) || !Number.isFinite(coordinateY)) return;
    const dx = coordinateX - x;
    const dy = coordinateY - y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    if (distance <= 16 && distance < bestDistance) {
      bestDistance = distance;
      best = {
        ...item,
        coordinateX,
        coordinateY,
        markerKey: `${item.tradeKey}-${item.side}`,
      };
    }
  });
  return best;
}

function tradeKey(trade) {
  return `${trade.takeProfitRate}-${trade.windowIndex}-${trade.levelIndex}-${trade.buyPoint?.time}-${trade.sellPoint?.time}`;
}

function formatTradeTime(value) {
  return formatBeijingDateTime(value);
}

function formatPercent(value) {
  const number = Number(value);
  return Number.isFinite(number)
    ? `${(number * 100)
        .toFixed(2)
        .replace(/\.00$/, "")
        .replace(/(\.\d)0$/, "$1")}%`
    : "-";
}

function toggleIndicator(key) {
  enabledIndicators.value = enabledIndicators.value.includes(key)
    ? enabledIndicators.value.filter((item) => item !== key)
    : [...enabledIndicators.value, key];
}

function setActiveTool(tool) {
  activeTool.value = tool;
  pendingTrendPoint.value = null;
  isSelecting.value = false;
}

function clearDrawings() {
  drawings.value = [];
  pendingTrendPoint.value = null;
  renderPriceLines();
  touchOverlay();
}

function syncIndicatorSeries() {
  if (!chart || !series) return;
  removeIndicatorSeries();
  if (enabledIndicators.value.includes("vol")) {
    volumeSeries = chart.addSeries(HistogramSeries, {
      priceScaleId: "volume",
      priceFormat: { type: "volume" },
      lastValueVisible: false,
      priceLineVisible: false,
    });
    chart.priceScale("volume").applyOptions({
      scaleMargins: { top: 0.76, bottom: 0 },
      borderVisible: false,
    });
    volumeSeries.setData(
      chartKlines.value.map((item) => ({
        time: item.time,
        value: item.volume > 0 ? item.volume : 0,
        color:
          item.close >= item.open
            ? "rgba(20, 184, 106, 0.36)"
            : "rgba(239, 68, 68, 0.36)",
      })),
    );
  }
  appendLineIndicator(
    "ma7",
    calculateSMA(chartKlines.value, 7),
    indicatorColors.ma7,
    "MA7",
  );
  appendLineIndicator(
    "ma20",
    calculateSMA(chartKlines.value, 20),
    indicatorColors.ma20,
    "MA20",
  );
  appendLineIndicator(
    "ma60",
    calculateSMA(chartKlines.value, 60),
    indicatorColors.ma60,
    "MA60",
  );
  appendLineIndicator(
    "ema20",
    calculateEMA(chartKlines.value, 20),
    indicatorColors.ema20,
    "EMA20",
  );
  if (enabledIndicators.value.includes("boll")) {
    const bands = calculateBollinger(chartKlines.value, 20, 2);
    appendLineIndicator(
      "bollUpper",
      bands.upper,
      indicatorColors.bollUpper,
      "BOLL U",
    );
    appendLineIndicator(
      "bollMiddle",
      bands.middle,
      indicatorColors.bollMiddle,
      "BOLL M",
    );
    appendLineIndicator(
      "bollLower",
      bands.lower,
      indicatorColors.bollLower,
      "BOLL L",
    );
  }
}

function appendLineIndicator(key, data, color, title) {
  const baseKey = key.startsWith("boll") ? "boll" : key;
  if (!enabledIndicators.value.includes(baseKey)) return;
  if (!data.length) return;
  const line = chart.addSeries(LineSeries, {
    color,
    lineWidth: 1.6,
    crosshairMarkerVisible: false,
    lastValueVisible: false,
    priceLineVisible: false,
    title,
  });
  line.setData(data);
  indicatorSeries.set(key, line);
}

function removeIndicatorSeries() {
  if (volumeSeries) {
    chart.removeSeries(volumeSeries);
    volumeSeries = null;
  }
  indicatorSeries.forEach((line) => chart.removeSeries(line));
  indicatorSeries.clear();
}

function calculateSMA(items, period) {
  const result = [];
  let sum = 0;
  items.forEach((item, index) => {
    sum += item.close;
    if (index >= period) {
      sum -= items[index - period].close;
    }
    if (index >= period - 1) {
      result.push({ time: item.time, value: sum / period });
    }
  });
  return result;
}

function calculateEMA(items, period) {
  if (!items.length) return [];
  const multiplier = 2 / (period + 1);
  let previous = items[0].close;
  return items.map((item, index) => {
    if (index === 0) {
      previous = item.close;
    } else {
      previous = (item.close - previous) * multiplier + previous;
    }
    return { time: item.time, value: previous };
  });
}

function calculateBollinger(items, period, multiplier) {
  const upper = [];
  const middle = [];
  const lower = [];
  for (let index = period - 1; index < items.length; index += 1) {
    const slice = items.slice(index - period + 1, index + 1);
    const mean = slice.reduce((total, item) => total + item.close, 0) / period;
    const variance =
      slice.reduce((total, item) => total + (item.close - mean) ** 2, 0) /
      period;
    const std = Math.sqrt(variance);
    upper.push({ time: items[index].time, value: mean + std * multiplier });
    middle.push({ time: items[index].time, value: mean });
    lower.push({ time: items[index].time, value: mean - std * multiplier });
  }
  return { upper, middle, lower };
}

function mapDrawingToCoordinates(drawing) {
  if (!chart || !series) return null;
  if (drawing.type === "horizontal") {
    const y = series.priceToCoordinate(drawing.price);
    if (!Number.isFinite(y)) return null;
    return {
      id: drawing.id,
      type: drawing.type,
      color: drawingColors.horizontal,
      x1: 0,
      y1: y,
      x2: overlaySize.value.width,
      y2: y,
      labelX: 10,
      labelY: Math.max(14, y - 8),
      label: `H · ${formatMarketCap(drawing.price)}`,
    };
  }
  const startX = chart.timeScale().timeToCoordinate(drawing.start.time);
  const endX = chart.timeScale().timeToCoordinate(drawing.end.time);
  const startY = series.priceToCoordinate(drawing.start.price);
  const endY = series.priceToCoordinate(drawing.end.price);
  if (![startX, endX, startY, endY].every((value) => Number.isFinite(value)))
    return null;
  return {
    id: drawing.id,
    type: drawing.type,
    color: drawingColors.trend,
    x1: startX,
    y1: startY,
    x2: endX,
    y2: endY,
    labelX: endX + 8,
    labelY: endY - 8,
    label: "Trend",
  };
}

function touchOverlay() {
  overlayRevision.value += 1;
}

onMounted(() => {
  if (!chartEl.value) return;
  chart = createChart(chartEl.value, {
    width: chartEl.value.clientWidth,
    height: 520,
    attributionLogo: false,
    layout: {
      background: { color: "transparent" },
      textColor: "#d8e9e2",
      fontFamily: "Fira Sans, Avenir Next, sans-serif",
    },
    grid: {
      vertLines: { color: "rgba(143,178,168,0.08)" },
      horzLines: { color: "rgba(143,178,168,0.12)" },
    },
    localization: {
      locale: "zh-CN",
      priceFormatter: (value) => formatMarketCap(value),
      timeFormatter: (value) => formatChartCrosshairTime(value),
    },
    crosshair: { mode: 0 },
    rightPriceScale: {
      borderColor: "#2b4a4d",
      scaleMargins: { top: 0.08, bottom: 0.26 },
    },
    timeScale: {
      borderColor: "#2b4a4d",
      timeVisible: true,
      secondsVisible: false,
      tickMarkFormatter: (value) => formatChartTick(value),
    },
  });
  series = chart.addSeries(CandlestickSeries, {
    upColor: "#14b86a",
    downColor: "#ef4444",
    wickUpColor: "#14b86a",
    wickDownColor: "#ef4444",
    borderVisible: false,
    priceFormat: { type: "price", precision: 2, minMove: 0.01 },
  });
  markerApi = createSeriesMarkers(series, [], { zOrder: "top" });
  resizeObserver = new ResizeObserver(() => {
    chart?.applyOptions({
      width: chartEl.value?.clientWidth || 0,
      height: chartEl.value?.clientHeight || 520,
    });
    touchOverlay();
  });
  resizeObserver.observe(chartWrapEl.value || chartEl.value);
  chart.timeScale().subscribeVisibleLogicalRangeChange(() => {
    touchOverlay();
  });
  renderChart();
});

watch(
  () => [
    props.klines,
    props.levels,
    props.selectedLevel,
    props.strategyTrades,
    props.focusedTradeKey,
  ],
  () => {
    hoveredTradeMarker.value = null;
    renderChart();
  },
  { deep: true },
);

watch(
  enabledIndicators,
  () => {
    renderChart();
  },
  { deep: true },
);

watch(
  drawings,
  () => {
    touchOverlay();
  },
  { deep: true },
);

onBeforeUnmount(() => {
  removeIndicatorSeries();
  priceLines.forEach((line) => series?.removePriceLine(line));
  priceLines = [];
  if (hoverPriceLine) {
    series?.removePriceLine(hoverPriceLine);
    hoverPriceLine = null;
  }
  markerApi?.detach();
  markerApi = null;
  resizeObserver?.disconnect();
  chart?.remove();
});
</script>

<style scoped>
.chart-shell {
  position: relative;
  display: grid;
  gap: 10px;
}

.chart-toolbar,
.chart-status-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.chart-toolbar__group,
.chart-hint-group {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.chart-toolbar__label {
  color: rgba(216, 233, 226, 0.7);
  font-size: 12px;
  font-weight: 700;
}

.toolbar-chip {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.58);
  color: #cfe1db;
  font-size: 12px;
  line-height: 1;
  padding: 7px 10px;
  cursor: pointer;
  transition: all 0.18s ease;
}

.toolbar-chip:hover {
  border-color: rgba(56, 189, 248, 0.45);
  color: #f8fafc;
}

.toolbar-chip.active {
  border-color: rgba(56, 189, 248, 0.55);
  background: rgba(14, 165, 233, 0.16);
  color: #f8fafc;
}

.toolbar-chip.danger:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.level-strip {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.chart-hint-group {
  color: rgba(216, 233, 226, 0.7);
  font-size: 12px;
}

.chart-wrap {
  position: relative;
}

.chart-canvas {
  min-height: 520px;
}

.drawing-overlay {
  position: absolute;
  inset: 0;
  z-index: 3;
  pointer-events: none;
}

.drawing-label {
  font-size: 12px;
  font-weight: 700;
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
  touch-action: none;
  user-select: none;
}

.selection-layer.tool-range {
  cursor: crosshair;
}

.selection-layer.tool-horizontal,
.selection-layer.tool-trend {
  cursor: cell;
}

.selection-rect {
  position: absolute;
  top: 0;
  bottom: 0;
  border-left: 1px solid rgba(56, 189, 248, 0.9);
  border-right: 1px solid rgba(56, 189, 248, 0.9);
  background: linear-gradient(
    180deg,
    rgba(56, 189, 248, 0.18),
    rgba(14, 165, 233, 0.06)
  );
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

.chart-empty {
  color: rgba(216, 233, 226, 0.7);
  font-size: 13px;
}

@media (max-width: 900px) {
  .chart-toolbar,
  .chart-status-bar,
  .level-strip {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
