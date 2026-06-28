import { defineStore } from "pinia";
import {
  candidateMonitorStreamURL,
  closeTradePosition,
  createBacktest,
  fetchSupportResistance,
  fetchTradeRuntime,
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
    tradeSignals: [],
    candidateMonitorItems: [],
    tradeOrders: [],
    tradePositions: [],
    tradeStreamSources: [],
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
        const [signals, candidates, orders, positions] = await Promise.all([
          listTradeSignals(params),
          listCandidateMonitor(),
          listTradeOrders(params),
          listTradePositions({ ...params, status: params.status || "" }),
        ]);
        this.tradeSignals = signals.items || [];
        this.candidateMonitorItems = candidates.items || [];
        this.tradeOrders = orders.items || [];
        this.tradePositions = positions.items || [];
        return {
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

    startTradeStreams(params = {}) {
      this.stopTradeStreams();
      if (typeof EventSource === "undefined") return;
      const streamParams = { tradeMode: params.tradeMode || "all", limit: params.limit || 50 };
      this.openTradeStream(candidateMonitorStreamURL(), "candidateMonitorItems", "tokenAddress", compareCandidates);
      this.openTradeStream(tradeSignalsStreamURL(streamParams), "tradeSignals", "id", compareSignals);
      this.openTradeStream(tradeOrdersStreamURL(streamParams), "tradeOrders", "id", compareOrders);
      this.openTradeStream(tradePositionsStreamURL({ ...streamParams, status: params.status || "" }), "tradePositions", "id", comparePositions);
    },
    stopTradeStreams() {
      this.tradeStreamSources.forEach((source) => source.close());
      this.tradeStreamSources = [];
    },
    openTradeStream(url, stateKey, idKey, compareFn) {
      const source = new EventSource(url);
      source.addEventListener("snapshot", (event) => {
        const data = parseStreamData(event);
        this[stateKey] = [...(data.items || [])].sort(compareFn);
      });
      source.addEventListener("upsert", (event) => {
        const data = parseStreamData(event);
        if (!data.item) return;
        this[stateKey] = upsertSorted(this[stateKey], data.item, idKey, compareFn);
      });
      source.addEventListener("delete", (event) => {
        const data = parseStreamData(event);
        if (!data.id) return;
        this[stateKey] = this[stateKey].filter((item) => String(item[idKey]) !== String(data.id));
      });
      source.onerror = () => {
        this.error = "实时数据连接异常，浏览器会自动重连";
      };
      this.tradeStreamSources.push(source);
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

function parseStreamData(event) {
  try {
    return JSON.parse(event.data || "{}");
  } catch {
    return {};
  }
}

function upsertSorted(items, nextItem, idKey, compareFn) {
  const nextID = String(nextItem[idKey]);
  const merged = items.filter((item) => String(item[idKey]) !== nextID);
  merged.push(nextItem);
  return merged.sort(compareFn);
}

function timestamp(value) {
  const time = new Date(value || 0).getTime();
  return Number.isFinite(time) ? time : 0;
}

function compareCandidates(left, right) {
  return timestamp(right.candidateAt) - timestamp(left.candidateAt);
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
