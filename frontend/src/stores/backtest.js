import { defineStore } from 'pinia'
import { createBacktest, fetchBirdeyeSupportResistance, listBacktests, listStrategyBacktestMethods, runStrategyBacktest } from '../api/backtest.js'

export const useBacktestStore = defineStore('backtest', {
  state: () => ({
    loading: false,
    backtestLoading: false,
    error: '',
    result: null,
    sessions: [],
    strategyMethods: [],
    strategyBacktestResult: null
  }),
  actions: {
    async loadKlineLevels(params) {
      this.loading = true
      this.error = ''
      try {
        const data = await fetchBirdeyeSupportResistance(params)
        this.result = {
          klines: data.klines || [],
          windows: data.windows || [],
          windowSize: data.windowSize || 0,
          windowStep: data.windowStep || 1
        }
        return this.result
      } catch (error) {
        this.error = error.message
        throw error
      } finally {
        this.loading = false
      }
    },
    async run(payload) {
      this.loading = true
      this.error = ''
      try {
        this.result = await createBacktest(payload)
        return this.result
      } catch (error) {
        this.error = error.message
        throw error
      } finally {
        this.loading = false
      }
    },
    async refreshSessions() {
      const data = await listBacktests()
      this.sessions = data.items || []
    },
    async loadStrategyMethods() {
      const data = await listStrategyBacktestMethods()
      this.strategyMethods = data.items || []
      return this.strategyMethods
    },
    async runStrategyBacktest(payload) {
      this.backtestLoading = true
      this.error = ''
      try {
        const data = await runStrategyBacktest(payload)
        this.result = {
          klines: data.klines || [],
          windows: data.windows || [],
          windowSize: payload.levelOptions?.windowSize || 0,
          windowStep: 1
        }
        this.strategyBacktestResult = data
        return data
      } catch (error) {
        this.error = error.message
        throw error
      } finally {
        this.backtestLoading = false
      }
    }
  }
})
