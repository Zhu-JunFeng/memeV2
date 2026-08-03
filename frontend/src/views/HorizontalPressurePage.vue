<template>
  <main class="horizontal-page">
    <section class="horizontal-hero">
      <div>
        <p class="horizontal-kicker">pivot high clustering · system 1m</p>
        <h1>水平压力位聚类</h1>
        <p>
          使用成熟的 Swing High/Pivot High 方法找局部顶点，把相近价格聚类成水平压力带，再按{{ analysisModeLabel }}标出 B/S。
        </p>
      </div>
      <div class="horizontal-method-card">
        <span>{{ analysisModeLabel }}</span>
        <strong>{{ analysisMode === "replay" ? "确认 Pivot → 历史聚类 → 当前K线突破 → 回放卖出" : "Pivot High → 价格聚类 → 水平压力带 → 突破 B/S" }}</strong>
        <em>{{ analysisMode === "replay" ? "逐根K线向前推演，B点不会使用右侧未来K线形成的压力位" : "适合观察全量历史里反复出现的水平压力区间" }}</em>
      </div>
    </section>

    <section class="horizontal-controls">
      <label class="control-field control-field-token">
        <span>Token CA</span>
        <el-input v-model="tokenAddress" clearable />
      </label>
      <label class="control-field">
        <span>数据源</span>
        <el-select v-model="dataSource">
          <el-option label="系统K线" value="system" />
          <el-option label="Birdeye" value="birdeye" />
          <el-option label="GMGN" value="gmgn" />
        </el-select>
      </label>
      <label class="control-field">
        <span>周期</span>
        <el-select v-model="interval">
          <el-option label="1m" value="1m" />
          <el-option label="5m" value="5m" />
          <el-option label="15m" value="15m" />
          <el-option label="1h" value="1h" />
        </el-select>
      </label>
      <label class="control-field">
        <span>模式</span>
        <el-select v-model="analysisMode">
          <el-option label="真实回放" value="replay" />
          <el-option label="历史观察" value="historical" />
        </el-select>
      </label>
      <label class="control-field">
        <span>Pivot窗口</span>
        <el-input-number v-model="pivotWindow" :min="1" :step="1" />
      </label>
      <label class="control-field">
        <span>聚类带宽(%)</span>
        <el-input-number v-model="mergePercentInput" :min="0.1" :step="0.1" :precision="1" />
      </label>
      <label class="control-field">
        <span>触压次数</span>
        <el-input-number v-model="minTouches" :min="2" :step="1" />
      </label>
      <label class="control-field">
        <span>回看小时</span>
        <el-input-number v-model="lookbackHours" :min="0" :step="1" />
      </label>
      <label class="control-field">
        <span>回测金额($)</span>
        <el-input-number v-model="positionSizeUsdInput" :min="1" :step="10" :precision="2" />
      </label>
      <label class="control-field">
        <span>买入滑点(%)</span>
        <el-input-number v-model="buySlippagePercentInput" :min="0" :step="0.1" :precision="2" />
      </label>
      <label class="control-field">
        <span>卖出滑点(%)</span>
        <el-input-number v-model="sellSlippagePercentInput" :min="0" :step="0.1" :precision="2" />
      </label>
      <el-button type="primary" size="large" :loading="loading" @click="loadKlines">
        加载并计算
      </el-button>
    </section>

    <section class="metric-ribbon horizontal-ribbon">
      <div>
        <span>模式</span>
        <strong>{{ analysisModeLabel }}</strong>
      </div>
      <div>
        <span>K线数量</span>
        <strong>{{ normalizedKlines.length }}</strong>
      </div>
      <div>
        <span>Pivot High</span>
        <strong>{{ analysis.pivots.length }}</strong>
      </div>
      <div>
        <span>水平压力带</span>
        <strong>{{ analysis.levels.length }}</strong>
      </div>
      <div>
        <span>B/S交易</span>
        <strong>{{ analysis.trades.length }}</strong>
      </div>
      <div>
        <span>胜率</span>
        <strong>{{ winRateLabel }}</strong>
      </div>
      <div>
        <span>净收益</span>
        <strong :class="strategyProfitUsd >= 0 ? 'profit-text' : 'loss-text'">${{ formatUsd(strategyProfitUsd) }}</strong>
      </div>
      <div>
        <span>时间范围</span>
        <strong>{{ rangeLabel }}</strong>
      </div>
    </section>

    <section class="horizontal-layout">
      <div class="chart-panel horizontal-chart-panel">
        <div class="chart-head">
          <div>
            <strong>水平压力带叠加</strong>
            <span>{{ chartExplainText }}</span>
          </div>
          <div class="legend-row">
            <button v-if="selectedTrade" type="button" class="clear-trade-button" @click="clearSelectedTrade">显示全部 B/S</button>
            <span class="legend-item upper">压力带</span>
            <span class="legend-item pivot">Pivot</span>
            <span class="legend-item touch">触压</span>
            <span class="legend-item buy">B 买入</span>
            <span class="legend-item sell">S 卖出</span>
          </div>
        </div>
        <div ref="chartBoxEl" class="horizontal-chart-box">
          <div ref="chartEl" class="horizontal-chart"></div>
          <svg class="horizontal-pressure-overlay" :width="overlaySize.width" :height="overlaySize.height">
            <g v-for="band in visibleBandRects" :key="band.id">
              <rect
                :x="0"
                :y="band.y"
                :width="overlaySize.width"
                :height="band.height"
                class="horizontal-band"
                :style="{ opacity: band.opacity }"
              />
              <text :x="12" :y="Math.max(14, band.y - 4)" class="horizontal-band-label">
                R{{ band.rank }} {{ formatMarketCap(band.center) }}
              </text>
            </g>
            <g
              v-for="badge in visibleTradeBadges"
              :key="badge.id"
              :class="['trade-badge', `trade-badge-${badge.type}`, badge.tone, selectedTrade ? 'trade-badge-focus' : '']"
            >
              <line
                :x1="badge.anchorX"
                :y1="badge.anchorY"
                :x2="badge.x"
                :y2="badge.y"
                class="trade-badge-line"
              />
              <circle
                :cx="badge.anchorX"
                :cy="badge.anchorY"
                :r="badge.dotRadius"
                class="trade-price-dot-outer"
              />
              <circle
                :cx="badge.anchorX"
                :cy="badge.anchorY"
                :r="badge.dotRadius - 2"
                class="trade-price-dot-inner"
              />
              <rect
                :x="badge.x - badge.width / 2"
                :y="badge.y - 13"
                :width="badge.width"
                :height="26"
                :rx="13"
                class="trade-badge-box"
              />
              <text :x="badge.x" :y="badge.y - 1" class="trade-badge-title">{{ badge.title }}</text>
              <text :x="badge.x" :y="badge.y + 9" class="trade-badge-price">{{ badge.price }}</text>
            </g>
          </svg>
          <div v-if="loading" class="chart-loading">正在加载 K 线...</div>
          <div v-else-if="!normalizedKlines.length" class="chart-loading">暂无 K 线数据</div>
        </div>
      </div>

      <aside class="insight-panel horizontal-insight-panel">
        <div class="insight-section">
          <span class="insight-label">当前策略 B/S</span>
          <strong>{{ strategySummaryLabel }}</strong>
          <p>{{ strategyExplainText }}</p>
        </div>
        <div class="insight-section">
          <span class="insight-label">交易列表</span>
          <div v-if="orderedTrades.length" class="trade-list">
            <button
              v-for="(trade, index) in orderedTrades"
              :key="trade.id"
              type="button"
              :class="['trade-row', selectedTrade?.id === trade.id ? 'active' : '']"
              @click="selectTrade(trade)"
            >
              <span>#{{ index + 1 }}</span>
              <strong :class="trade.profitUsd >= 0 ? 'profit-text' : 'loss-text'">${{ formatUsd(trade.profitUsd) }}</strong>
              <em>B {{ formatShortTime(trade.buyPoint.openTime) }} · S {{ formatShortTime(trade.sellPoint.openTime) }}</em>
              <small>{{ formatPercent(trade.profitRate) }} · {{ trade.exitReason }}</small>
            </button>
          </div>
          <p v-else class="empty-note">当前参数下暂无可列出的交易。</p>
        </div>
        <div v-if="selectedTrade" class="insight-section trade-detail-section">
          <span class="insight-label">选中交易计算明细</span>
          <strong :class="selectedTrade.profitUsd >= 0 ? 'profit-text' : 'loss-text'">
            {{ formatPercent(selectedTrade.profitRate) }} · ${{ formatUsd(selectedTrade.profitUsd) }}
          </strong>
          <div class="trade-fact-grid">
            <div>
              <span>B执行</span>
              <b>{{ formatMarketCap(selectedTrade.buyPoint.price) }}</b>
              <em>{{ formatShortTime(selectedTrade.buyPoint.openTime) }}</em>
            </div>
            <div>
              <span>B触发</span>
              <b>{{ formatMarketCap(selectedTrade.buyPoint.triggerPrice || selectedTrade.level.breakoutThreshold) }}</b>
              <em>压力上沿 + {{ formatPercent(strategyOptions.breakTolerance) }}</em>
            </div>
            <div>
              <span>S执行</span>
              <b>{{ formatMarketCap(selectedTrade.sellPoint.price) }}</b>
              <em>{{ selectedTrade.exitReason }}</em>
            </div>
            <div>
              <span>压力带</span>
              <b>{{ formatMarketCap(selectedTrade.level.lower) }} - {{ formatMarketCap(selectedTrade.level.upper) }}</b>
              <em>pivot {{ selectedTrade.level.pivotCount }} · 触压 {{ selectedTrade.touches.length }}</em>
            </div>
          </div>
          <div class="calculation-list">
            <span class="calculation-title">参与触压 K 线</span>
            <button
              v-for="touch in selectedTrade.touches"
              :key="`touch-${selectedTrade.id}-${touch.index}`"
              type="button"
              @click="scrollToTime(touch.time)"
            >
              <em>{{ formatShortTime(touch.openTime) }}</em>
              <strong>高 {{ formatMarketCap(touch.price) }}</strong>
              <small>#{{ touch.index }}</small>
            </button>
          </div>
          <div class="calculation-list">
            <span class="calculation-title">参与压力带 Pivot</span>
            <button
              v-for="pivot in selectedTradePivots"
              :key="`pivot-${selectedTrade.id}-${pivot.index}`"
              type="button"
              @click="scrollToTime(pivot.time)"
            >
              <em>{{ formatShortTime(pivot.openTime) }}</em>
              <strong>高 {{ formatMarketCap(pivot.price) }}</strong>
              <small>{{ pivot.confirmedOpenTime ? `确认 ${formatShortTime(pivot.confirmedOpenTime)}` : `#${pivot.index}` }}</small>
            </button>
          </div>
        </div>
        <div class="insight-section">
          <span class="insight-label">{{ analysisMode === "replay" ? "回放压力带" : "最强压力带" }}</span>
          <div v-if="analysis.levels.length" class="level-list">
            <button
              v-for="level in analysis.levels.slice(0, 10)"
              :key="level.id"
              type="button"
              class="level-row"
              @click="focusLevel(level)"
            >
              <span>R{{ level.rank }}</span>
              <strong>{{ formatMarketCap(level.lower) }} - {{ formatMarketCap(level.upper) }}</strong>
              <em>pivot {{ level.pivotCount }} · 触压 {{ level.touchCount }} · {{ level.replayGeneratedAtOpenTime ? `B前形成 ${formatShortTime(level.replayGeneratedAtOpenTime)}` : formatFixed(level.score) }}</em>
            </button>
          </div>
          <p v-else class="empty-note">当前参数下没有形成有效水平压力带。</p>
        </div>
        <div class="insight-section">
          <span class="insight-label">最近 B/S</span>
          <div v-if="recentTrades.length" class="event-list">
            <button
              v-for="trade in recentTrades"
              :key="trade.id"
              type="button"
              class="event-row trade-event-row"
              @click="selectTrade(trade)"
            >
              <span :class="['event-dot', trade.profitRate >= 0 ? 'profit' : 'loss']"></span>
              <span>B/S</span>
              <strong>{{ formatPercent(trade.profitRate) }}</strong>
              <em>{{ formatShortTime(trade.buyPoint.openTime) }}</em>
            </button>
          </div>
          <p v-else class="empty-note">当前水平压力带没有跑出符合策略的 B/S 交易。</p>
        </div>
      </aside>
    </section>
  </main>
</template>

<script setup>
import {
  CandlestickSeries,
  createChart,
  createSeriesMarkers,
} from "lightweight-charts";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { fetchKlines } from "../api/backtest.js";
import {
  HORIZONTAL_PRESSURE_DEFAULTS,
  analyzeHorizontalPressure,
  analyzeHorizontalPressureReplay,
} from "../utils/horizontalPressure.js";
import {
  formatBeijingDateTime,
  formatChartCrosshairTime,
  formatChartTick,
  toUnixTimestamp,
} from "../utils/time.js";

const DEFAULT_TOKEN = "7V6Sk63y8Rr1MvcN5mYNp61wgFhy4EeQg5gUASk9pump";

const tokenAddress = ref(DEFAULT_TOKEN);
const dataSource = ref("system");
const interval = ref("1m");
const analysisMode = ref("replay");
const lookbackHours = ref(24);
const pivotWindow = ref(HORIZONTAL_PRESSURE_DEFAULTS.pivotWindow);
const mergePercentInput = ref(HORIZONTAL_PRESSURE_DEFAULTS.mergePercent * 100);
const minTouches = ref(HORIZONTAL_PRESSURE_DEFAULTS.minTouches);
const positionSizeUsdInput = ref(HORIZONTAL_PRESSURE_DEFAULTS.positionSizeUsd);
const buySlippagePercentInput = ref(HORIZONTAL_PRESSURE_DEFAULTS.buySlippageRate * 100);
const sellSlippagePercentInput = ref(HORIZONTAL_PRESSURE_DEFAULTS.sellSlippageRate * 100);
const loading = ref(false);
const rawKlines = ref([]);
const selectedTradeId = ref("");
const overlayRevision = ref(0);
const chartEl = ref(null);
const chartBoxEl = ref(null);
let chart;
let candleSeries;
let markerApi;
let resizeObserver;

const strategyOptions = computed(() => ({
  ...HORIZONTAL_PRESSURE_DEFAULTS,
  pivotWindow: pivotWindow.value,
  mergePercent: mergePercentInput.value / 100,
  minTouches: minTouches.value,
  positionSizeUsd: positionSizeUsdInput.value,
  buySlippageRate: buySlippagePercentInput.value / 100,
  sellSlippageRate: sellSlippagePercentInput.value / 100,
}));

const normalizedKlines = computed(() => {
  const byTime = new Map();
  rawKlines.value.forEach((item) => {
    const normalized = normalizeKline(item);
    if (normalized) byTime.set(normalized.time, normalized);
  });
  return [...byTime.values()].sort((left, right) => left.time - right.time);
});

const candles = computed(() =>
  normalizedKlines.value.map((item) => ({
    time: item.time,
    open: item.open,
    high: item.high,
    low: item.low,
    close: item.close,
  })),
);

const analysis = computed(() =>
  analysisMode.value === "replay"
    ? analyzeHorizontalPressureReplay(normalizedKlines.value, strategyOptions.value)
    : analyzeHorizontalPressure(normalizedKlines.value, strategyOptions.value),
);
const recentTrades = computed(() => analysis.value.trades.slice(-10).reverse());
const orderedTrades = computed(() => [...analysis.value.trades].sort((left, right) => left.buyIndex - right.buyIndex));
const selectedTrade = computed(() => analysis.value.trades.find((trade) => trade.id === selectedTradeId.value) || null);
const markerTrades = computed(() => (selectedTrade.value ? [selectedTrade.value] : analysis.value.trades));
const chartLevels = computed(() => (selectedTrade.value ? [selectedTrade.value.level] : analysis.value.levels));
const selectedTradePivots = computed(() => selectedTrade.value?.level?.pivots || []);
const strategyProfitRate = computed(() =>
  analysis.value.trades.reduce((total, trade) => total + Number(trade.profitRate || 0), 0),
);
const strategyProfitUsd = computed(() =>
  analysis.value.trades.reduce((total, trade) => total + Number(trade.profitUsd || 0), 0),
);
const winRateLabel = computed(() => {
  if (!analysis.value.trades.length) return "-";
  const wins = analysis.value.trades.filter((trade) => Number(trade.profitRate || 0) > 0).length;
  return formatPercent(wins / analysis.value.trades.length);
});
const strategySummaryLabel = computed(() => {
  if (!analysis.value.trades.length) return "暂无交易";
  return `${analysis.value.trades.length} 笔 · 胜率 ${winRateLabel.value} · 净 $${formatUsd(strategyProfitUsd.value)}`;
});
const analysisModeLabel = computed(() => (analysisMode.value === "replay" ? "真实回放" : "历史观察"));
const chartExplainText = computed(() =>
  analysisMode.value === "replay"
    ? "橙色横带 = 回放中当时已确认的压力带；黄色点 = 已确认 pivot high；触压/B/S 均来自逐根K线回测"
    : "橙色横带 = 全量历史聚类出的水平压力带；黄色点 = pivot high；B/S = 历史观察买卖点",
);
const strategyExplainText = computed(() =>
  analysisMode.value === "replay"
    ? `真实回放逐根处理K线：当前K线只使用此前已确认的 pivot high 和触压记录；满足 ${minTouches.value} 次阳线触压后，后续 ${minTouches.value} 根 K 线内收盘突破压力带上沿 +1% 标 B；B执行价叠加买入滑点，S执行价扣卖出滑点。`
    : `历史观察会用全量K线先聚类压力带；满足 ${minTouches.value} 次阳线触压后，后续 ${minTouches.value} 根 K 线内收盘突破压力带上沿 +1% 标 B；B/S收益按当前回测金额和滑点计算。`,
);
const rangeLabel = computed(() => {
  if (!normalizedKlines.value.length) return "-";
  const first = normalizedKlines.value[0];
  const last = normalizedKlines.value[normalizedKlines.value.length - 1];
  return `${formatShortTime(first.openTime)} - ${formatShortTime(last.openTime)}`;
});
const overlaySize = computed(() => ({
  width: chartEl.value?.clientWidth || 0,
  height: chartEl.value?.clientHeight || 560,
}));
const visibleBandRects = computed(() => {
  overlayRevision.value;
  if (!chart || !candleSeries || !overlaySize.value.width) return [];
  return chartLevels.value
    .map((level) => {
      const yUpper = candleSeries.priceToCoordinate(level.upper);
      const yLower = candleSeries.priceToCoordinate(level.lower);
      if (![yUpper, yLower].every(Number.isFinite)) return null;
      const y = Math.min(yUpper, yLower);
      const height = Math.max(3, Math.abs(yLower - yUpper));
      if (y > overlaySize.value.height || y + height < 0) return null;
      return {
        id: level.id,
        rank: level.rank,
        center: level.center,
        y,
        height,
        opacity: Math.max(0.08, 0.28 - (level.rank - 1) * 0.012),
      };
    })
    .filter(Boolean);
});
const visibleTradeBadges = computed(() => {
  overlayRevision.value;
  if (!chart || !candleSeries || !overlaySize.value.width) return [];
  return markerTrades.value
    .flatMap((trade) => [
      buildTradeBadge(trade, "buy", trade.buyPoint, "B 买入"),
      buildTradeBadge(trade, "sell", trade.sellPoint, "S 卖出"),
    ])
    .filter(Boolean);
});

async function loadKlines() {
  const token = tokenAddress.value.trim();
  if (!token) return;
  loading.value = true;
  try {
    const data = await fetchKlines({ source: dataSource.value, tokenAddress: token, interval: interval.value, ...queryTimeRange() });
    rawKlines.value = data.items || [];
    await nextTick();
    renderChart(true);
  } finally {
    loading.value = false;
  }
}

function queryTimeRange() {
  const hours = Number(lookbackHours.value);
  if (!Number.isFinite(hours) || hours <= 0) return {};
  const end = new Date();
  const start = new Date(end.getTime() - hours * 60 * 60 * 1000);
  return { startTime: start.toISOString(), endTime: end.toISOString() };
}

function normalizeKline(item) {
  const time = toUnixTimestamp(item?.openTime);
  const open = firstFinite(item?.marketCapOpen, item?.open);
  const high = firstFinite(item?.marketCapHigh, item?.high);
  const low = firstFinite(item?.marketCapLow, item?.low);
  const close = firstFinite(item?.marketCapClose, item?.close);
  if (!time || [open, high, low, close].some((value) => !Number.isFinite(value) || value <= 0)) return null;
  return { time, openTime: item.openTime, open, high, low, close, volume: Number(item.volume || 0) };
}

function firstFinite(...values) {
  for (const value of values) {
    const number = Number(value);
    if (Number.isFinite(number) && number > 0) return number;
  }
  return Number.NaN;
}

function initChart() {
  if (!chartEl.value || chart) return;
  chart = createChart(chartEl.value, {
    width: chartEl.value.clientWidth,
    height: 560,
    attributionLogo: false,
    layout: {
      background: { color: "transparent" },
      textColor: "#d8e9e2",
      fontFamily: "Avenir Next, PingFang SC, sans-serif",
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
    rightPriceScale: { borderColor: "#2b4a4d", scaleMargins: { top: 0.08, bottom: 0.12 } },
    timeScale: {
      borderColor: "#2b4a4d",
      timeVisible: true,
      secondsVisible: false,
      tickMarkFormatter: (value) => formatChartTick(value),
    },
  });
  candleSeries = chart.addSeries(CandlestickSeries, {
    upColor: "#18c779",
    downColor: "#ef4444",
    wickUpColor: "#18c779",
    wickDownColor: "#ef4444",
    borderVisible: false,
    priceFormat: { type: "price", precision: 2, minMove: 0.01 },
  });
  markerApi = createSeriesMarkers(candleSeries, [], { zOrder: "top" });
  resizeObserver = new ResizeObserver(() => {
    chart?.applyOptions({ width: chartEl.value?.clientWidth || 0, height: chartEl.value?.clientHeight || 560 });
    touchOverlay();
  });
  resizeObserver.observe(chartBoxEl.value || chartEl.value);
  chart.timeScale().subscribeVisibleLogicalRangeChange(touchOverlay);
}

function renderChart(fit = false) {
  if (!chart || !candleSeries) return;
  candleSeries.setData(candles.value);
  markerApi.setMarkers([...buildPivotMarkers(), ...buildTouchMarkers(), ...buildTradeMarkers()].sort((left, right) => left.time - right.time));
  if (fit) chart.timeScale().fitContent();
  touchOverlay();
}

function buildPivotMarkers() {
  const pivots = selectedTrade.value ? selectedTradePivots.value : analysis.value.pivots;
  return pivots.map((pivot) => ({
    time: pivot.time,
    position: "aboveBar",
    color: "#facc15",
    shape: "circle",
    text: selectedTrade.value ? `P ${formatMarketCap(pivot.price)}` : "",
    size: selectedTrade.value ? 0.75 : 0.45,
  }));
}

function buildTouchMarkers() {
  const byTime = new Map();
  markerTrades.value.forEach((trade) => {
    (trade.touches || []).forEach((touch) => {
      if (!byTime.has(touch.time)) byTime.set(touch.time, touch);
    });
  });
  return [...byTime.values()].map((touch) => ({
    time: touch.time,
    position: "aboveBar",
    color: "#fb923c",
    shape: "square",
    text: "触",
    size: 0.75,
  }));
}

function buildTradeMarkers() {
  return markerTrades.value.flatMap((trade) => [
    {
      time: trade.buyPoint.time,
      position: "belowBar",
      color: "#22c55e",
      shape: "arrowUp",
      text: "B",
      size: selectedTrade.value ? 1.35 : 1.05,
    },
    {
      time: trade.sellPoint.time,
      position: "aboveBar",
      color: trade.profitRate >= 0 ? "#38bdf8" : "#ef4444",
      shape: "arrowDown",
      text: "S",
      size: selectedTrade.value ? 1.35 : 1.05,
    },
  ]);
}

function buildTradeBadge(trade, type, point, title) {
  const timeScale = chart?.timeScale?.();
  const anchorX = timeScale?.timeToCoordinate?.(point?.time);
  const anchorY = candleSeries?.priceToCoordinate?.(point?.price);
  if (![anchorX, anchorY].every(Number.isFinite)) return null;
  const focused = Boolean(selectedTrade.value);
  const width = focused ? 78 : 62;
  const offsetY = type === "buy" ? 32 : -32;
  return {
    id: `${trade.id}-${type}`,
    type,
    tone: type === "sell" && trade.profitRate < 0 ? "trade-badge-loss" : "",
    title,
    price: formatMarketCap(point.price),
    anchorX,
    anchorY,
    dotRadius: focused ? 6 : 4.5,
    width,
    x: clamp(anchorX, width / 2 + 6, overlaySize.value.width - width / 2 - 6),
    y: clamp(anchorY + offsetY, 24, overlaySize.value.height - 24),
  };
}

function selectTrade(trade) {
  selectedTradeId.value = trade.id;
  nextTick(() => {
    renderChart(false);
    scrollToTrade(trade);
  });
}

function clearSelectedTrade() {
  selectedTradeId.value = "";
  nextTick(() => renderChart(false));
}

function scrollToTrade(trade) {
  if (!chart || !trade) return;
  const indexes = [
    trade.buyIndex,
    trade.sellIndex,
    ...(trade.touches || []).map((item) => item.index),
    ...(trade.level?.pivots || []).map((item) => item.index),
  ].filter((value) => Number.isFinite(Number(value)));
  if (!indexes.length) return;
  const from = Math.max(0, Math.min(...indexes) - 16);
  const to = Math.max(...indexes) + 18;
  chart.timeScale().setVisibleLogicalRange({ from, to });
  touchOverlay();
}

function scrollToTime(time) {
  if (!chart || !time) return;
  const index = normalizedKlines.value.findIndex((item) => item.time === time);
  if (index < 0) return;
  chart.timeScale().setVisibleLogicalRange({ from: Math.max(0, index - 80), to: index + 40 });
  touchOverlay();
}

function focusLevel(level) {
  const pivot = level.pivots?.at(-1) || level.touches?.at(-1);
  if (pivot?.time) scrollToTime(pivot.time);
}

function touchOverlay() {
  requestAnimationFrame(() => {
    overlayRevision.value += 1;
  });
}

function formatMarketCap(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  if (Math.abs(number) >= 1_000_000) return `${trimDecimals(number / 1_000_000)}m`;
  if (Math.abs(number) >= 1_000) return `${trimDecimals(number / 1_000)}k`;
  return trimDecimals(number);
}

function trimDecimals(value) {
  return Number(value).toFixed(2).replace(/\.00$/, "").replace(/(\.\d)0$/, "$1");
}

function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

function formatFixed(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number.toFixed(2) : "-";
}

function formatPercent(value) {
  const number = Number(value);
  return Number.isFinite(number) ? `${(number * 100).toFixed(2)}%` : "-";
}

function formatUsd(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number.toFixed(2) : "-";
}

function formatShortTime(value) {
  const text = formatBeijingDateTime(value);
  return text ? text.slice(5, 16) : "-";
}

watch([analysis, normalizedKlines, selectedTradeId], () => {
  if (selectedTradeId.value && !selectedTrade.value) selectedTradeId.value = "";
  renderChart(false);
});

onMounted(() => {
  initChart();
  loadKlines();
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  chart?.remove();
  chart = null;
});
</script>
