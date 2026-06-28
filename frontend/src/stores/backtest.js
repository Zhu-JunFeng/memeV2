import { defineStore } from "pinia";
import {
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
