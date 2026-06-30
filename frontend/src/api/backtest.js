import { http } from "./http.js";

export function searchTokens(keyword) {
  return http.get("/tokens/search", { params: { keyword } });
}

export function fetchKlines(params) {
  return http.get("/market/klines", { params });
}

export function fetchSupportResistance(params) {
  return http.get("/market/support-resistance", { params });
}

export function listStrategyBacktestMethods() {
  return http.get("/strategy-backtests/methods");
}

export function runStrategyBacktest(payload) {
  return http.post("/strategy-backtests/run", payload, {
    timeout: 180000,
  });
}

export function createBacktest(payload) {
  return http.post("/backtests", payload);
}

export function listBacktests() {
  return http.get("/backtests");
}

export function getBacktest(id) {
  return http.get(`/backtests/${id}`);
}

export function fetchTradeRuntime() {
  return http.get("/trade/runtime");
}

export function updateTradeRuntime(payload) {
  return http.put("/trade/runtime", payload);
}

export function listTradeSummary() {
  return http.get("/trade/summary");
}

export function listTradeSignals(params) {
  return http.get("/trade/signals", { params });
}

export function getTradeSignal(id) {
  return http.get(`/trade/signals/${id}`);
}

export function getTradeSignalBySignalId(signalId) {
  return http.get(`/trade/signals/by-signal-id/${encodeURIComponent(signalId)}`);
}

export function listCandidateMonitor() {
  return http.get("/signal/candidate-monitor");
}

export function addCandidateMonitor(tokenAddress) {
  return http.post("/signal/candidate-monitor", { tokenAddress });
}

export function listTradeOrders(params) {
  return http.get("/trade/orders", { params });
}

export function getTradeOrder(id) {
  return http.get(`/trade/orders/${id}`);
}

export function retryTradeOrder(id) {
  return http.post(`/trade/orders/${id}/retry`);
}

export function listTradePositions(params) {
  return http.get("/trade/positions", { params });
}

export function closeTradePosition(id) {
  return http.post(`/trade/positions/${id}/close`);
}

function streamURL(path, params = {}) {
  const query = new URLSearchParams();
  Object.entries(params || {}).forEach(([key, value]) => {
    if (value === undefined || value === null || value === "") return;
    query.set(key, value);
  });
  const suffix = query.toString();
  return `/api${path}${suffix ? `?${suffix}` : ""}`;
}

export function candidateMonitorStreamURL() {
  return streamURL("/signal/candidate-monitor/stream");
}

export function tradeSignalsStreamURL(params) {
  return streamURL("/trade/signals/stream", params);
}

export function tradeOrdersStreamURL(params) {
  return streamURL("/trade/orders/stream", params);
}

export function tradePositionsStreamURL(params) {
  return streamURL("/trade/positions/stream", params);
}
