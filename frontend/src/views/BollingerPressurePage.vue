<template>
  <main class="boll-page">
    <section class="boll-hero">
      <div>
        <p class="boll-kicker">pandas_ta.bbands · system 1m</p>
        <h1>BOLL 动态压力带</h1>
        <p>
          使用 pandas_ta 布林带口径计算上轨压力区，把每一根有效 K 线的
          <strong>中轨 - 上轨</strong> 动态压力带完整叠加到 K 线图上。
        </p>
      </div>
      <div class="formula-card">
        <span>计算公式</span>
        <strong>BBU = SMA(close, N) + σ × STD(close, N)</strong>
        <em>默认 N=20，σ=2，ddof=0，价格使用市值 close</em>
      </div>
    </section>

    <section class="boll-controls">
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
        <span>Length</span>
        <el-input-number v-model="bollLength" :min="2" :step="1" />
      </label>
      <label class="control-field">
        <span>Std</span>
        <el-input-number v-model="bollStd" :min="0.1" :step="0.1" :precision="1" />
      </label>
      <label class="control-field">
        <span>ddof</span>
        <el-input-number v-model="bollDdof" :min="0" :step="1" />
      </label>
      <label class="control-field">
        <span>回看小时</span>
        <el-input-number v-model="lookbackHours" :min="0" :step="1" />
      </label>
      <el-button type="primary" size="large" :loading="loading" @click="loadKlines">
        加载并计算
      </el-button>
    </section>

    <section class="metric-ribbon">
      <div>
        <span>K线数量</span>
        <strong>{{ normalizedKlines.length }}</strong>
      </div>
      <div>
        <span>BOLL 点数</span>
        <strong>{{ bollBands.length }}</strong>
      </div>
      <div>
        <span>压力触及</span>
        <strong>{{ pressureTouchCount }}</strong>
      </div>
      <div>
        <span>上轨突破</span>
        <strong>{{ pressureBreakCount }}</strong>
      </div>
      <div>
        <span>B/S交易</span>
        <strong>{{ bollStrategyTrades.length }}</strong>
      </div>
      <div>
        <span>净收益</span>
        <strong :class="strategyProfitRate >= 0 ? 'profit-text' : 'loss-text'">{{ formatPercent(strategyProfitRate) }}</strong>
      </div>
      <div>
        <span>时间范围</span>
        <strong>{{ rangeLabel }}</strong>
      </div>
    </section>

    <section class="boll-layout">
      <div class="chart-panel boll-chart-panel">
        <div class="chart-head">
          <div>
            <strong>动态压力带叠加</strong>
            <span>红色半透明区域 = 每根 K 线对应的 BOLL 中轨到上轨</span>
          </div>
          <div class="legend-row">
            <span class="legend-item upper">上轨/压力</span>
            <span class="legend-item middle">中轨</span>
            <span class="legend-item lower">下轨</span>
            <span class="legend-item break">突破</span>
            <span class="legend-item buy">B 买入</span>
            <span class="legend-item sell">S 卖出</span>
          </div>
        </div>
        <div ref="chartBoxEl" class="boll-chart-box">
          <div ref="chartEl" class="boll-chart"></div>
          <svg
            class="boll-pressure-overlay"
            :width="overlaySize.width"
            :height="overlaySize.height"
          >
            <path v-if="pressureAreaPath" :d="pressureAreaPath" class="pressure-area" />
          </svg>
          <div v-if="loading" class="chart-loading">正在加载 K 线...</div>
          <div v-else-if="!normalizedKlines.length" class="chart-loading">暂无 K 线数据</div>
        </div>
      </div>

      <aside class="insight-panel">
        <div class="insight-section">
          <span class="insight-label">当前压力带</span>
          <strong>{{ latestBandLabel }}</strong>
          <p>{{ latestBandDetail }}</p>
        </div>
        <div class="insight-section">
          <span class="insight-label">计算口径</span>
          <ul>
            <li>先按开盘时间去重并升序排列。</li>
            <li>使用市值 close 作为 pandas_ta close 输入。</li>
            <li>std 采用 rolling standard deviation，默认 ddof=0。</li>
            <li>压力带为 BOLL 中轨到上轨，不做额外兜底数据源。</li>
          </ul>
        </div>
        <div class="insight-section">
          <span class="insight-label">当前策略 B/S</span>
          <strong>{{ strategySummaryLabel }}</strong>
          <p>使用 BOLL 中轨-上轨作为压力带，满足 {{ strategyConfig.minTouches }} 次阳线触压后，后续 {{ strategyConfig.minTouches }} 根 K 线内收盘突破冻结上轨 + {{ formatPercent(strategyConfig.breakTolerance) }} 时标 B；卖出沿用固定 5% 止损、8% 止盈和 7%/4% 动态止盈。</p>
          <div v-if="recentStrategyTrades.length" class="event-list">
            <button
              v-for="trade in recentStrategyTrades"
              :key="trade.id"
              type="button"
              class="event-row trade-event-row"
              @click="scrollToTime(trade.buyPoint.time)"
            >
              <span :class="['event-dot', trade.profitRate >= 0 ? 'profit' : 'loss']"></span>
              <span>B/S</span>
              <strong>{{ formatPercent(trade.profitRate) }}</strong>
              <em>{{ formatShortTime(trade.buyPoint.openTime) }}</em>
            </button>
          </div>
          <p v-else class="empty-note">当前 BOLL 压力带没有跑出符合当前策略的 B/S 交易。</p>
        </div>
        <div class="insight-section">
          <span class="insight-label">最近压力事件</span>
          <div v-if="recentPressureEvents.length" class="event-list">
            <button
              v-for="event in recentPressureEvents"
              :key="`${event.time}-${event.kind}`"
              type="button"
              class="event-row"
              @click="scrollToTime(event.time)"
            >
              <span :class="['event-dot', event.kind]"></span>
              <span>{{ event.kind === 'breakout' ? '突破上轨' : '触及压力' }}</span>
              <strong>{{ formatMarketCap(event.price) }}</strong>
              <em>{{ formatShortTime(event.openTime) }}</em>
            </button>
          </div>
          <p v-else class="empty-note">当前参数下没有触及/突破上轨事件。</p>
        </div>
      </aside>
    </section>
  </main>
</template>

<script setup>
import {
  CandlestickSeries,
  LineSeries,
  createChart,
  createSeriesMarkers,
} from "lightweight-charts";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { fetchKlines } from "../api/backtest.js";
import { calculatePandasTaBbands } from "../utils/bollinger.js";
import {
  BOLLINGER_STRATEGY_DEFAULTS,
  simulateBollingerPressureTrades,
} from "../utils/bollingerStrategy.js";
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
const lookbackHours = ref(0);
const bollLength = ref(20);
const bollStd = ref(2);
const bollDdof = ref(0);
const strategyConfig = BOLLINGER_STRATEGY_DEFAULTS;
const loading = ref(false);
const rawKlines = ref([]);
const overlayRevision = ref(0);
const chartEl = ref(null);
const chartBoxEl = ref(null);

let chart;
let candleSeries;
let upperSeries;
let middleSeries;
let lowerSeries;
let markerApi;
let resizeObserver;

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

const bollBands = computed(() =>
  calculatePandasTaBbands(normalizedKlines.value, {
    length: bollLength.value,
    std: bollStd.value,
    ddof: bollDdof.value,
    valueAccessor: (item) => item.close,
  }),
);

const bandByTime = computed(
  () => new Map(bollBands.value.map((item) => [item.time, item])),
);

const pressureEvents = computed(() =>
  normalizedKlines.value
    .map((kline) => {
      const band = bandByTime.value.get(kline.time);
      if (!band || !Number.isFinite(band.upper)) return null;
      if (kline.close > band.upper) {
        return {
          kind: "breakout",
          time: kline.time,
          openTime: kline.openTime,
          price: kline.close,
          band,
        };
      }
      if (kline.high >= band.upper) {
        return {
          kind: "touch",
          time: kline.time,
          openTime: kline.openTime,
          price: kline.high,
          band,
        };
      }
      return null;
    })
    .filter(Boolean),
);

const pressureTouchCount = computed(
  () => pressureEvents.value.filter((item) => item.kind === "touch").length,
);
const pressureBreakCount = computed(
  () => pressureEvents.value.filter((item) => item.kind === "breakout").length,
);
const recentPressureEvents = computed(() => pressureEvents.value.slice(-12).reverse());
const bollStrategyTrades = computed(() =>
  simulateBollingerPressureTrades(normalizedKlines.value, bollBands.value, strategyConfig),
);
const recentStrategyTrades = computed(() => bollStrategyTrades.value.slice(-8).reverse());
const strategyProfitRate = computed(() =>
  bollStrategyTrades.value.reduce((total, trade) => total + Number(trade.profitRate || 0), 0),
);
const strategySummaryLabel = computed(() => {
  if (!bollStrategyTrades.value.length) return "暂无交易";
  const wins = bollStrategyTrades.value.filter((trade) => Number(trade.profitRate || 0) > 0).length;
  return `${bollStrategyTrades.value.length} 笔 · 胜率 ${formatPercent(wins / bollStrategyTrades.value.length)} · 净 ${formatPercent(strategyProfitRate.value)}`;
});
const overlaySize = computed(() => ({
  width: chartEl.value?.clientWidth || 0,
  height: chartEl.value?.clientHeight || 560,
}));

const pressureAreaPath = computed(() => {
  overlayRevision.value;
  if (!chart || !candleSeries || !overlaySize.value.width || !bollBands.value.length) {
    return "";
  }
  const upperPoints = [];
  const middlePoints = [];
  const width = overlaySize.value.width;
  bollBands.value.forEach((band) => {
    const x = chart.timeScale().timeToCoordinate(band.time);
    const yUpper = candleSeries.priceToCoordinate(band.upper);
    const yMiddle = candleSeries.priceToCoordinate(band.middle);
    if (![x, yUpper, yMiddle].every(Number.isFinite)) return;
    if (x < -80 || x > width + 80) return;
    upperPoints.push([x, yUpper]);
    middlePoints.push([x, yMiddle]);
  });
  if (upperPoints.length < 2) return "";
  const upperPath = upperPoints.map(([x, y], index) => `${index === 0 ? "M" : "L"}${round(x)} ${round(y)}`).join(" ");
  const lowerPath = middlePoints
    .reverse()
    .map(([x, y]) => `L${round(x)} ${round(y)}`)
    .join(" ");
  return `${upperPath} ${lowerPath} Z`;
});

const rangeLabel = computed(() => {
  if (!normalizedKlines.value.length) return "-";
  const first = normalizedKlines.value[0];
  const last = normalizedKlines.value[normalizedKlines.value.length - 1];
  return `${formatShortTime(first.openTime)} - ${formatShortTime(last.openTime)}`;
});

const latestBand = computed(() => bollBands.value[bollBands.value.length - 1] || null);
const latestBandLabel = computed(() => {
  if (!latestBand.value) return "-";
  return `${formatMarketCap(latestBand.value.middle)} - ${formatMarketCap(latestBand.value.upper)}`;
});
const latestBandDetail = computed(() => {
  if (!latestBand.value) return "加载 K 线后显示最新一根 K 线对应的压力带。";
  return `最新 BOLL 带宽 ${formatFixed(latestBand.value.bandwidth)}%，%B ${formatFixed(latestBand.value.percent)}。`;
});

async function loadKlines() {
  const token = tokenAddress.value.trim();
  if (!token) return;
  loading.value = true;
  try {
    const data = await fetchKlines({
      source: dataSource.value,
      tokenAddress: token,
      interval: interval.value,
      ...queryTimeRange(),
    });
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
  if (!time || [open, high, low, close].some((value) => !Number.isFinite(value) || value <= 0)) {
    return null;
  }
  return {
    time,
    openTime: item.openTime,
    open,
    high,
    low,
    close,
    volume: Number(item.volume || 0),
  };
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
    rightPriceScale: {
      borderColor: "#2b4a4d",
      scaleMargins: { top: 0.08, bottom: 0.12 },
    },
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
  upperSeries = addLine("#f97316", "BOLL 上轨/压力");
  middleSeries = addLine("#9fb6b0", "BOLL 中轨");
  lowerSeries = addLine("#22c55e", "BOLL 下轨");
  markerApi = createSeriesMarkers(candleSeries, [], { zOrder: "top" });

  resizeObserver = new ResizeObserver(() => {
    chart?.applyOptions({
      width: chartEl.value?.clientWidth || 0,
      height: chartEl.value?.clientHeight || 560,
    });
    touchOverlay();
  });
  resizeObserver.observe(chartBoxEl.value || chartEl.value);
  chart.timeScale().subscribeVisibleLogicalRangeChange(touchOverlay);
}

function addLine(color, title) {
  return chart.addSeries(LineSeries, {
    color,
    lineWidth: 1.6,
    crosshairMarkerVisible: false,
    lastValueVisible: true,
    priceLineVisible: false,
    title,
  });
}

function renderChart(fit = false) {
  if (!chart || !candleSeries) return;
  candleSeries.setData(candles.value);
  upperSeries.setData(bollBands.value.map((item) => ({ time: item.time, value: item.upper })));
  middleSeries.setData(bollBands.value.map((item) => ({ time: item.time, value: item.middle })));
  lowerSeries.setData(bollBands.value.map((item) => ({ time: item.time, value: item.lower })));
  markerApi.setMarkers(
    [...buildPressureMarkers(), ...buildTradeMarkers()].sort((left, right) => left.time - right.time),
  );
  if (fit) chart.timeScale().fitContent();
  touchOverlay();
}

function buildPressureMarkers() {
  return pressureEvents.value.map((event) => ({
    time: event.time,
    position: "aboveBar",
    color: event.kind === "breakout" ? "#38bdf8" : "#facc15",
    shape: "circle",
    text: "",
    size: event.kind === "breakout" ? 0.7 : 0.5,
  }));
}

function buildTradeMarkers() {
  return bollStrategyTrades.value.flatMap((trade) => [
    {
      time: trade.buyPoint.time,
      position: "belowBar",
      color: "#22c55e",
      shape: "arrowUp",
      text: "B",
      size: 1.25,
    },
    {
      time: trade.sellPoint.time,
      position: "aboveBar",
      color: trade.profitRate >= 0 ? "#38bdf8" : "#ef4444",
      shape: "arrowDown",
      text: "S",
      size: 1.25,
    },
  ]);
}

function scrollToTime(time) {
  if (!chart || !time) return;
  const index = normalizedKlines.value.findIndex((item) => item.time === time);
  if (index < 0) return;
  chart.timeScale().setVisibleLogicalRange({
    from: Math.max(0, index - 80),
    to: index + 40,
  });
  touchOverlay();
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
  return Number(value)
    .toFixed(2)
    .replace(/\.00$/, "")
    .replace(/(\.\d)0$/, "$1");
}

function formatFixed(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number.toFixed(2) : "-";
}

function formatPercent(value) {
  const number = Number(value);
  return Number.isFinite(number) ? `${(number * 100).toFixed(2)}%` : "-";
}

function formatShortTime(value) {
  const text = formatBeijingDateTime(value);
  return text ? text.slice(5, 16) : "-";
}

function round(value) {
  return Math.round(value * 10) / 10;
}

watch([bollBands, pressureEvents, bollStrategyTrades], () => renderChart(false));

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
