import { http } from "./http.js";

export function searchTokens(keyword) {
  return http.get("/tokens/search", { params: { keyword } });
}

export function fetchKlines(params) {
  return http.get("/market/klines", { params });
}

export function fetchBirdeyeSupportResistance(params) {
  return http.get("/market/birdeye/support-resistance", { params });
}

export function listStrategyBacktestMethods() {
  return http.get("/strategy-backtests/methods");
}

export function runStrategyBacktest(payload) {
  return http.post("/strategy-backtests/run", payload);
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

export function listTradeSignals(params) {
  return http.get("/trade/signals", { params });
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
