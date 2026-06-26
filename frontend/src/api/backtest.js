import { http } from './http.js'

export function searchTokens(keyword) {
  return http.get('/tokens/search', { params: { keyword } })
}

export function fetchKlines(params) {
  return http.get('/market/klines', { params })
}

export function fetchBirdeyeSupportResistance(params) {
  return http.get('/market/birdeye/support-resistance', { params })
}

export function listStrategyBacktestMethods() {
  return http.get('/strategy-backtests/methods')
}

export function runStrategyBacktest(payload) {
  return http.post('/strategy-backtests/run', payload)
}

export function createBacktest(payload) {
  return http.post('/backtests', payload)
}

export function listBacktests() {
  return http.get('/backtests')
}

export function getBacktest(id) {
  return http.get(`/backtests/${id}`)
}
