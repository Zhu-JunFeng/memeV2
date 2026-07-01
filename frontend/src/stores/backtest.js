import { defineStore } from "pinia";
import { ElNotification } from "element-plus";
import {
  addCandidateMonitor,
  candidateMonitorStreamURL,
  closeTradePosition,
  createBacktest,
  deleteCandidateMonitor,
  fetchKlines,
  fetchSupportResistance,
  fetchTradeRuntime,
  getTradeSignal,
  getTradeSignalBySignalId,
  listTradeSummary,
  listBacktests,
  listCandidateMonitor,
  listStrategyBacktestMethods,
  listTradeOrders,
  listTradePositions,
  listTradeSignals,
  retryTradeOrder,
  tradeOrdersStreamURL,
  tradePositionsStreamURL,
  tradeSignalsStreamURL,
  runStrategyBacktest,
  updateTradeRuntime,
} from "../api/backtest.js";

let lastStreamAlertAt = 0;
const TRADE_STREAM_DEFAULT_LIMIT = 20;

export const useBacktestStore = defineStore("backtest", {
  state: () => ({
    loading: false,
    backtestLoading: false,
    tradeLoading: false,
    runtimeUpdating: false,
    error: "",
    result: null,
    sessions: [],
    strategyMethods: [],
    strategyBacktestResult: null,
    tradeRuntime: {
      tradeMode: "paper",
      options: [],
    },
    tradeSummaryItems: [],
    tradeSignals: [],
    candidateMonitorItems: [],
    tradeOrders: [],
    tradePositions: [],
    tradeStreamSources: [],
    activeTradeStreamTab: "",
  }),
  actions: {
    async loadKlineLevels(params) {
      this.loading = true;
      this.error = "";
      try {
        const data = await fetchSupportResistance(params);
        this.result = {
          klines: data.klines || [],
          windows: data.windows || [],
          windowSize: data.windowSize || 0,
          windowStep: data.windowStep || 1,
        };
        return this.result;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.loading = false;
      }
    },
    async loadRawKlines(params) {
      this.loading = true;
      this.error = "";
      try {
        const data = await fetchKlines(params);
        this.result = {
          klines: data.items || [],
          windows: [],
          windowSize: 0,
          windowStep: 1,
        };
        return this.result;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.loading = false;
      }
    },
    async run(payload) {
      this.loading = true;
      this.error = "";
      try {
        this.result = await createBacktest(payload);
        return this.result;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.loading = false;
      }
    },
    async refreshSessions() {
      const data = await listBacktests();
      this.sessions = data.items || [];
    },
    async loadStrategyMethods() {
      const data = await listStrategyBacktestMethods();
      this.strategyMethods = data.items || [];
      return this.strategyMethods;
    },
    async runStrategyBacktest(payload) {
      this.backtestLoading = true;
      this.error = "";
      try {
        const data = await runStrategyBacktest(payload);
        this.result = {
          klines: data.klines || [],
          windows: data.windows || [],
          windowSize: payload.levelOptions?.windowSize || 0,
          windowStep: 1,
        };
        this.strategyBacktestResult = data;
        return data;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.backtestLoading = false;
      }
    },
    async loadTradeRuntime() {
      const data = await fetchTradeRuntime();
      this.tradeRuntime = {
        tradeMode: data.tradeMode || "paper",
        options: data.options || [],
      };
      return this.tradeRuntime;
    },
    async setTradeMode(tradeMode) {
      this.runtimeUpdating = true;
      this.error = "";
      try {
        const data = await updateTradeRuntime({ tradeMode });
        this.tradeRuntime.tradeMode = data.tradeMode || tradeMode;
        return this.tradeRuntime.tradeMode;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.runtimeUpdating = false;
      }
    },
    async loadTradeDashboard(params = {}) {
      this.tradeLoading = true;
      this.error = "";
      try {
        const listLimit = params.limit || TRADE_STREAM_DEFAULT_LIMIT;
        const [summary, signals, candidates, orders, positions] = await Promise.all([
          listTradeSummary(),
          listTradeSignals(params),
          listCandidateMonitor(),
          listTradeOrders(params),
          listTradePositions({ ...params, status: params.status || "" }),
        ]);
        this.tradeSummaryItems = summary.items || [];
        this.tradeSignals = limitSortedItems(
          signals.items || [],
          compareSignals,
          listLimit,
        );
        this.candidateMonitorItems = limitSortedItems(
          candidates.items || [],
          compareCandidates,
        );
        this.tradeOrders = limitSortedItems(orders.items || [], compareOrders, listLimit);
        this.tradePositions = limitSortedItems(
          positions.items || [],
          comparePositions,
          listLimit,
        );
        return {
          summary: this.tradeSummaryItems,
          signals: this.tradeSignals,
          candidates: this.candidateMonitorItems,
          orders: this.tradeOrders,
          positions: this.tradePositions,
        };
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.tradeLoading = false;
      }
    },
    async fetchTradeSignal(id) {
      return getTradeSignal(id).then((data) => data.item);
    },
    async fetchTradeSignalBySignalId(signalId) {
      return getTradeSignalBySignalId(signalId).then((data) => data.item);
    },
    async addCandidateMonitor(tokenAddress) {
      this.tradeLoading = true;
      this.error = "";
      try {
        const data = await addCandidateMonitor(tokenAddress);
        if (data.item) {
          this.candidateMonitorItems = upsertSorted(
            this.candidateMonitorItems,
            data.item,
            "tokenAddress",
            compareCandidates,
          );
        }
        return data.item;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.tradeLoading = false;
      }
    },
    async deleteCandidateMonitor(tokenAddress) {
      this.tradeLoading = true;
      this.error = "";
      try {
        const data = await deleteCandidateMonitor(tokenAddress);
        this.candidateMonitorItems = this.candidateMonitorItems.filter(
          (item) => String(item.tokenAddress) !== String(tokenAddress),
        );
        return data.item;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.tradeLoading = false;
      }
    },

    startTradeStream(tab, params = {}) {
      this.stopTradeStreams();
      if (typeof EventSource === "undefined") return;
      const nextTab = String(tab || "").trim();
      if (!nextTab) return;
      const streamParams = {
        tradeMode: params.tradeMode || "all",
        limit: params.limit || TRADE_STREAM_DEFAULT_LIMIT,
      };
      const definitions = {
        candidates: {
          streams: [
            {
              url: candidateMonitorStreamURL(),
              stateKey: "candidateMonitorItems",
              idKey: "tokenAddress",
              compareFn: compareCandidates,
            },
          ],
        },
        signals: {
          streams: [
            {
              url: tradeSignalsStreamURL(streamParams),
              stateKey: "tradeSignals",
              idKey: "id",
              compareFn: compareSignals,
              maxItems: streamParams.limit,
            },
          ],
        },
        orders: {
          streams: [
            {
              url: tradeOrdersStreamURL(streamParams),
              stateKey: "tradeOrders",
              idKey: "id",
              compareFn: compareOrders,
              maxItems: streamParams.limit,
            },
          ],
        },
        positions: {
          streams: [
            {
              url: tradePositionsStreamURL({
                ...streamParams,
                status: params.status || "",
              }),
              stateKey: "tradePositions",
              idKey: "id",
              compareFn: comparePositions,
              maxItems: streamParams.limit,
            },
            {
              url: candidateMonitorStreamURL(),
              stateKey: "candidateMonitorItems",
              idKey: "tokenAddress",
              compareFn: compareCandidates,
            },
          ],
        },
      };
      const current = definitions[nextTab];
      if (!current) return;
      current.streams.forEach((item) => {
        this.openTradeStream(
          item.url,
          item.stateKey,
          item.idKey,
          item.compareFn,
          item.maxItems,
        );
      });
      this.activeTradeStreamTab = nextTab;
    },
    stopTradeStreams() {
      this.tradeStreamSources.forEach((source) => source.close());
      this.tradeStreamSources = [];
      this.activeTradeStreamTab = "";
    },
    openTradeStream(url, stateKey, idKey, compareFn, maxItems = 0) {
      const source = new EventSource(url);
      source.addEventListener("snapshot", (event) => {
        const data = parseStreamData(event);
        this[stateKey] = limitSortedItems(data.items || [], compareFn, maxItems);
      });
      source.addEventListener("upsert", (event) => {
        const data = parseStreamData(event);
        if (!data.item) return;
        this[stateKey] = upsertSorted(
          this[stateKey],
          data.item,
          idKey,
          compareFn,
          maxItems,
        );
        this.refreshTradeSummary(stateKey);
      });
      source.addEventListener("delete", (event) => {
        const data = parseStreamData(event);
        if (!data.id) return;
        this[stateKey] = this[stateKey].filter((item) => String(item[idKey]) !== String(data.id));
        this.refreshTradeSummary(stateKey);
      });
      source.onerror = () => {
        const message = "实时数据连接异常，浏览器会自动重连";
        this.error = message;
        notifyStreamError(message);
      };
      this.tradeStreamSources.push(source);
    },
    async refreshTradeSummary(triggerKey = "") {
      if (!["tradeSignals", "tradeOrders", "tradePositions"].includes(triggerKey)) return;
      try {
        const data = await listTradeSummary();
        this.tradeSummaryItems = data.items || [];
      } catch (error) {
        this.error = error.message;
      }
    },
    async retryTradeOrder(id, params = {}) {
      this.tradeLoading = true;
      this.error = "";
      try {
        const data = await retryTradeOrder(id);
        await this.loadTradeDashboard(params);
        return data;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.tradeLoading = false;
      }
    },
    async closeTradePosition(id, params = {}) {
      this.tradeLoading = true;
      this.error = "";
      try {
        const data = await closeTradePosition(id);
        await this.loadTradeDashboard(params);
        return data;
      } catch (error) {
        this.error = error.message;
        throw error;
      } finally {
        this.tradeLoading = false;
      }
    },
  },
});

function notifyStreamError(message) {
  const now = Date.now();
  if (now - lastStreamAlertAt < 5000) return;
  lastStreamAlertAt = now;
  ElNotification({
    title: "实时接口异常",
    message,
    type: "error",
    position: "top-right",
    duration: 7000,
    customClass: "api-error-notification",
  });
}

function parseStreamData(event) {
  try {
    return JSON.parse(event.data || "{}");
  } catch {
    return {};
  }
}

function limitSortedItems(items, compareFn, maxItems = 0) {
  const sorted = [...items].sort(compareFn);
  return maxItems > 0 ? sorted.slice(0, maxItems) : sorted;
}

function upsertSorted(items, nextItem, idKey, compareFn, maxItems = 0) {
  const nextID = String(nextItem[idKey]);
  const merged = items.filter((item) => String(item[idKey]) !== nextID);
  merged.push(nextItem);
  return limitSortedItems(merged, compareFn, maxItems);
}

function timestamp(value) {
  const time = new Date(value || 0).getTime();
  return Number.isFinite(time) ? time : 0;
}

function compareCandidates(left, right) {
  const byCandidateAt = timestamp(left.candidateAt) - timestamp(right.candidateAt);
  if (byCandidateAt !== 0) return byCandidateAt;
  return String(left.tokenAddress || "").localeCompare(String(right.tokenAddress || ""));
}

function compareSignals(left, right) {
  return timestamp(right.signalTime) - timestamp(left.signalTime);
}

function compareOrders(left, right) {
  return timestamp(right.createdAt) - timestamp(left.createdAt);
}

function comparePositions(left, right) {
  return timestamp(right.updatedAt) - timestamp(left.updatedAt);
}
