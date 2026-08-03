import { createRouter, createWebHistory } from 'vue-router'
import BacktestWorkspace from '../views/BacktestWorkspace.vue'
import BollingerPressurePage from '../views/BollingerPressurePage.vue'
import HorizontalPressurePage from '../views/HorizontalPressurePage.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'backtest', component: BacktestWorkspace },
    { path: '/bollinger-pressure', name: 'bollinger-pressure', component: BollingerPressurePage },
    { path: '/horizontal-pressure', name: 'horizontal-pressure', component: HorizontalPressurePage }
  ]
})
