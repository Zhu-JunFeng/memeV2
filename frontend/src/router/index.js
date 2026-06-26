import { createRouter, createWebHistory } from 'vue-router'
import BacktestWorkspace from '../views/BacktestWorkspace.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'backtest', component: BacktestWorkspace }
  ]
})
