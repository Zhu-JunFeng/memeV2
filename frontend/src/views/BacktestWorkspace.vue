<template>
  <main class="workspace">
    <header class="hero compact-hero">
      <el-button
        type="primary"
        size="large"
        :loading="store.loading"
        @click="loadKlineLevels"
        >加载 K 线并计算</el-button
      >
      <el-button
        size="large"
        :disabled="!selectedChartRange"
        :loading="store.loading"
        @click="loadSelectedRangeLevels"
        >计算选中区域压力位</el-button
      >
      <el-button
        size="large"
        :disabled="!selectedChartRange && !loadedRangeLabel"
        @click="clearSelectedRange"
        >恢复近 5 天</el-button
      >
      <el-button
        type="success"
        size="large"
        :loading="store.backtestLoading"
        @click="runStrategyForLoadedRange"
        >执行回测</el-button
      >
    </header>

    <section class="query-panel">
      <div class="query-overview">
        <div class="query-range-card">
          <span class="query-eyebrow">时间范围</span>
          <strong>{{ rangeCardTitle }}</strong>
          <div class="query-range-detail">{{ rangeCardDetail }}</div>
          <div class="query-range-meta">
            {{ loadedRangeLabel || "默认使用当前时刻向前近 5 天" }}
          </div>
        </div>
        <div class="query-view-card">
          <span class="query-eyebrow">查看类型</span>
          <el-segmented v-model="levelView" :options="levelViewOptions" />
        </div>
      </div>

      <div class="query-grid">
        <section class="query-group query-group-primary">
          <div class="query-group-title">基础参数</div>
          <div class="query-group-grid query-group-grid-primary">
            <label class="query-field query-field-wide">
              <span class="query-label">Token CA</span>
              <el-select
                v-model="form.tokenAddress"
                class="token-ca-select"
                filterable
                allow-create
                default-first-option
                clearable
                :reserve-keyword="false"
                placeholder="输入或选择 token CA"
              >
                <el-option
                  v-for="option in tokenAddressOptions"
                  :key="option.value"
                  :label="option.label"
                  :value="option.value"
                />
              </el-select>
            </label>
            <label class="query-field">
              <span class="query-label">K 线粒度</span>
              <el-select v-model="form.interval">
                <el-option label="1m" value="1m" />
                <el-option label="5m" value="5m" />
                <el-option label="15m" value="15m" />
                <el-option label="1h" value="1h" />
              </el-select>
            </label>
            <label class="query-field">
              <span class="query-label">数据源</span>
              <el-select v-model="form.dataSource">
                <el-option label="GMGN" value="gmgn" />
                <el-option label="Birdeye" value="birdeye" />
                <el-option label="SQL" value="sql" />
                <el-option label="DB" value="db" />
              </el-select>
            </label>
          </div>
        </section>

        <section class="query-group">
          <div class="query-group-title">窗口与带宽</div>
          <div class="query-group-grid">
            <label class="query-field">
              <span class="query-label">窗口K线数</span>
              <el-input-number
                v-model="form.windowSize"
                class="query-number"
                :min="20"
                :step="20"
              />
            </label>
            <label class="query-field">
              <span class="query-label">压力带窗口K线数</span>
              <el-input-number
                v-model="form.levelWindowSize"
                class="query-number"
                :min="20"
                :step="20"
              />
            </label>
            <label class="query-field">
              <span class="query-label">压力带窗口步长</span>
              <el-input-number
                v-model="form.levelWindowStep"
                class="query-number"
                :min="1"
                :step="1"
              />
            </label>
            <label class="query-field">
              <span class="query-label">带宽范围(%)</span>
              <el-input-number
                v-model="form.bandRangePercent"
                class="query-number"
                :min="0.1"
                :step="0.1"
                :precision="1"
              />
            </label>
          </div>
        </section>

        <section class="query-group">
          <div class="query-group-title">突破规则</div>
          <div class="query-group-grid query-group-grid-compact">
            <label class="query-field">
              <span class="query-label">n值(阳线/后续K线)</span>
              <el-input-number
                v-model="form.minTouches"
                class="query-number"
                :min="2"
                :step="1"
              />
            </label>
            <label class="query-field">
              <span class="query-label">突破确认根数</span>
              <el-input-number
                v-model="form.confirmBars"
                class="query-number"
                :min="1"
                :step="1"
              />
            </label>
          </div>
        </section>

        <section class="query-group query-group-strategy">
          <div class="query-group-title strategy-group-title-row">
            <span>回测方法</span>
            <span class="strategy-group-title-note">区间止盈 + 扣费净收益</span>
          </div>
          <div class="strategy-config-panel">
            <div class="strategy-topline">
              <label class="query-field strategy-method-select">
                <span class="query-label">方法</span>
                <el-select v-model="strategyForm.methodCode">
                  <el-option
                    v-for="method in strategyMethodOptions"
                    :key="method.code"
                    :label="method.name"
                    :value="method.code"
                  />
                </el-select>
              </label>
              <div class="strategy-overview-chips">
                <div class="strategy-overview-chip">
                  <span>止盈扫描</span>
                  <strong
                    >{{ strategyForm.takeProfitRateStartPercent }}% →
                    {{ strategyForm.takeProfitRateEndPercent }}%</strong
                  >
                  <em>每 {{ strategyForm.takeProfitRateStepPercent }}% 一组</em>
                </div>
                <div class="strategy-overview-chip">
                  <span>成本与仓位</span>
                  <strong
                    >{{ strategyForm.feeRatePercent }}% 费率 ·
                    {{ strategyForm.positionSizeUsd }}u</strong
                  >
                  <em
                    >硬止损 -{{ strategyForm.hardStopLossRatePercent }}% · 触发
                    {{ strategyForm.activationProfitRatePercent }}% / 锁盈
                    {{ strategyForm.lockedProfitRatePercent }}%</em
                  >
                </div>
              </div>
            </div>
            <div class="strategy-section-grid">
              <section class="strategy-field-section">
                <div class="strategy-section-head">
                  <strong>止盈扫描区间</strong>
                  <span>按区间批量测试不同止盈值</span>
                </div>
                <div class="strategy-field-grid">
                  <label class="query-field">
                    <span class="query-label">止盈起点(%)</span>
                    <el-input-number
                      v-model="strategyForm.takeProfitRateStartPercent"
                      class="query-number"
                      :min="1"
                      :max="50"
                      :step="1"
                    />
                  </label>
                  <label class="query-field">
                    <span class="query-label">止盈终点(%)</span>
                    <el-input-number
                      v-model="strategyForm.takeProfitRateEndPercent"
                      class="query-number"
                      :min="1"
                      :max="50"
                      :step="1"
                    />
                  </label>
                  <label class="query-field">
                    <span class="query-label">止盈步长(%)</span>
                    <el-input-number
                      v-model="strategyForm.takeProfitRateStepPercent"
                      class="query-number"
                      :min="0.5"
                      :max="10"
                      :step="0.5"
                      :precision="1"
                    />
                  </label>
                </div>
              </section>
              <section class="strategy-field-section">
                <div class="strategy-section-head">
                  <strong>仓位与风控</strong>
                  <span>控制手续费、单笔投入和动态止盈保护</span>
                </div>
                <div class="strategy-field-grid strategy-field-grid-risk">
                  <label class="query-field">
                    <span class="query-label">总手续费(%)</span>
                    <el-input-number
                      v-model="strategyForm.feeRatePercent"
                      class="query-number"
                      :min="0"
                      :max="30"
                      :step="0.1"
                      :precision="1"
                    />
                  </label>
                  <label class="query-field">
                    <span class="query-label">单笔投入(U)</span>
                    <el-input-number
                      v-model="strategyForm.positionSizeUsd"
                      class="query-number"
                      :min="1"
                      :step="1"
                    />
                  </label>
                  <label class="query-field">
                    <span class="query-label">硬止损(%)</span>
                    <el-input-number
                      v-model="strategyForm.hardStopLossRatePercent"
                      class="query-number"
                      :min="0.1"
                      :max="50"
                      :step="0.5"
                      :precision="1"
                    />
                  </label>
                  <label class="query-field">
                    <span class="query-label">动态止损触发(%)</span>
                    <el-input-number
                      v-model="strategyForm.activationProfitRatePercent"
                      class="query-number"
                      :min="1"
                      :max="50"
                      :step="1"
                    />
                  </label>
                  <label class="query-field">
                    <span class="query-label">动态锁盈(%)</span>
                    <el-input-number
                      v-model="strategyForm.lockedProfitRatePercent"
                      class="query-number"
                      :min="1"
                      :max="50"
                      :step="1"
                    />
                  </label>
                </div>
              </section>
            </div>
            <div class="strategy-caption" v-if="selectedStrategyMethod">
              {{ selectedStrategyMethod.description }}
            </div>
          </div>
        </section>
      </div>
      <el-alert
        v-if="store.error"
        type="error"
        :title="store.error"
        show-icon
      />
    </section>

    <section class="status-strip">
      <div class="status-item">
        <span>Source</span><strong>{{ dataSourceLabel }} K 线</strong>
      </div>
      <div class="status-item">
        <span>Interval</span><strong>{{ form.interval }}</strong>
      </div>
      <div class="status-item">
        <span>Bars</span><strong>{{ barCount }}</strong>
      </div>
      <div class="status-item">
        <span>Windows</span><strong>{{ windowCount }}</strong>
      </div>
      <div class="status-item">
        <span>Support</span><strong>{{ supportCount }}</strong>
      </div>
      <div class="status-item">
        <span>Resistance</span><strong>{{ resistanceCount }}</strong>
      </div>
      <div class="status-item">
        <span>Window</span><strong>{{ activeWindow }}</strong>
      </div>
    </section>

    <section class="panel trade-panel">
      <div class="panel-heading trade-panel-heading">
        <div>
          <div class="panel-title">交易模式与执行看板</div>
          <div class="panel-subtitle">
            全局模式落库保存。模拟盘仍走 Jupiter
            报价/下单准备，但不会执行链上签名与提交。
          </div>
        </div>
        <el-button
          size="small"
          :loading="store.tradeLoading"
          @click="refreshTradeDashboard"
          >刷新</el-button
        >
      </div>
      <div class="trade-runtime-grid">
        <div class="trade-runtime-card">
          <span class="trade-runtime-label">全局交易模式</span>
          <el-segmented
            v-model="tradeRuntimeMode"
            :options="tradeModeOptions"
            :disabled="store.runtimeUpdating"
            @change="handleTradeModeChange"
          />
          <div class="trade-runtime-hint">
            当前后端模式：
            <el-tag
              size="small"
              :type="modeTagType(store.tradeRuntime.tradeMode)"
              >{{ tradeModeText(store.tradeRuntime.tradeMode) }}</el-tag
            >
          </div>
        </div>
        <div class="trade-runtime-card">
          <span class="trade-runtime-label">列表筛选</span>
          <el-segmented
            v-model="tradeFilterMode"
            :options="tradeFilterOptions"
            @change="refreshTradeDashboard"
          />
          <div class="trade-runtime-hint">
            页面展示可按模拟盘 / 实盘 / 全部切换，不影响后端当前执行模式。
          </div>
        </div>
      </div>
      <div class="trade-kpis">
        <div class="trade-kpi">
          <span>Candidates</span>
          <strong>{{ store.candidateMonitorItems.length }}</strong>
        </div>
        <div class="trade-kpi">
          <span>Signals</span><strong>{{ store.tradeSignals.length }}</strong>
        </div>
        <div class="trade-kpi">
          <span>Orders</span><strong>{{ store.tradeOrders.length }}</strong>
        </div>
        <div class="trade-kpi">
          <span>Open</span><strong>{{ openTradePositions.length }}</strong>
        </div>
        <div class="trade-kpi">
          <span>Closed</span><strong>{{ closedTradePositions.length }}</strong>
        </div>
      </div>
      <el-tabs v-model="tradeTab" class="trade-tabs">
        <el-tab-pane label="Candidates" name="candidates">
          <el-table
            :data="store.candidateMonitorItems"
            size="small"
            stripe
            class="trade-table"
            table-layout="auto"
            empty-text="暂无上游候选项目"
          >
            <el-table-column label="状态" width="96">
              <template #default="{ row }">
                <el-tag size="small" :type="candidateStatusTagType(row.status)">
                  {{ candidateStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="Symbol / CA" min-width="220">
              <template #default="{ row }">
                <div class="trade-cell-stack">
                  <button
                    class="candidate-symbol-link"
                    type="button"
                    @click="loadCandidateSystemKlines(row)"
                  >
                    {{ row.symbol || "-" }}
                  </button>
                  <TokenAddressLink
                    :address="row.tokenAddress"
                    :short="true"
                    :compact="true"
                  />
                </div>
              </template>
            </el-table-column>
            <el-table-column label="上游评分" width="100">
              <template #default="{ row }">{{
                formatOptionalFixed(row.upstreamScore)
              }}</template>
            </el-table-column>
            <el-table-column label="上游市值" width="112">
              <template #default="{ row }">{{
                formatOptionalMarketCap(row.upstreamMarketCap)
              }}</template>
            </el-table-column>
            <el-table-column label="当前市值" width="112">
              <template #default="{ row }">
                {{ formatOptionalMarketCap(row.currentMarketCap) }}
              </template>
            </el-table-column>
            <el-table-column label="K线获取" width="104">
              <template #default="{ row }">{{
                formatRelativeTime(row.birdeyeKlineFetchedAt)
              }}</template>
            </el-table-column>
            <el-table-column label="压力带" width="180">
              <template #default="{ row }">
                <span v-if="row.levelMarketCap">
                  {{ formatMarketCap(row.levelLowerMarketCap) }} -
                  {{ formatMarketCap(row.levelUpperMarketCap) }}
                </span>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column label="入池时间" width="168">
              <template #default="{ row }">{{
                formatBeijingDateTime(row.candidateAt)
              }}</template>
            </el-table-column>
            <el-table-column label="买入信号" min-width="150">
              <template #default="{ row }">{{
                shortAddress(row.buySignalId || "-")
              }}</template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="Signals" name="signals">
          <el-table
            :data="store.tradeSignals"
            size="small"
            stripe
            class="trade-table"
            table-layout="auto"
            empty-text="暂无信号"
          >
            <el-table-column label="模式" width="92">
              <template #default="{ row }">
                <el-tag size="small" :type="modeTagType(row.tradeMode)">{{
                  tradeModeText(row.tradeMode)
                }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="signalType" label="方向" width="72" />
            <el-table-column label="Token / 周期" min-width="220">
              <template #default="{ row }">
                <div class="trade-cell-stack">
                  <TokenAddressLink
                    :address="row.tokenAddress"
                    :short="true"
                    :compact="true"
                  />
                  <span>{{ row.interval }} · {{ row.strategyCode }}</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="触发价位" width="120">
              <template #default="{ row }">{{
                formatMarketCap(row.triggerMarketCap)
              }}</template>
            </el-table-column>
            <el-table-column label="时间" width="168">
              <template #default="{ row }">{{
                formatBeijingDateTime(row.signalTime)
              }}</template>
            </el-table-column>
            <el-table-column prop="consumeStatus" label="状态" width="96" />
            <el-table-column
              prop="reason"
              label="原因"
              min-width="280"
              show-overflow-tooltip
            />
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="Orders" name="orders">
          <el-table
            :data="store.tradeOrders"
            size="small"
            stripe
            class="trade-table"
            table-layout="auto"
            empty-text="暂无订单"
          >
            <el-table-column label="模式" width="92">
              <template #default="{ row }">
                <el-tag size="small" :type="modeTagType(row.tradeMode)">{{
                  tradeModeText(row.tradeMode)
                }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="side" label="方向" width="72" />
            <el-table-column label="执行通道" width="126">
              <template #default="{ row }">{{
                row.executionChannel || "-"
              }}</template>
            </el-table-column>
            <el-table-column label="Token" min-width="180">
              <template #default="{ row }">
                <TokenAddressLink
                  :address="row.tokenAddress"
                  :short="true"
                  :compact="true"
                />
              </template>
            </el-table-column>
            <el-table-column label="计划金额" width="104">
              <template #default="{ row }">{{
                row.intentAmountUsd
                  ? formatUsd(row.intentAmountUsd).replace("+", "")
                  : formatTokenAmount(row.intentTokenAmount)
              }}</template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="90" />
            <el-table-column label="Tx" min-width="160">
              <template #default="{ row }">
                <div v-if="row.submitTxHash" class="tx-cell">
                  <span :title="row.submitTxHash">{{
                    shortAddress(row.submitTxHash)
                  }}</span>
                  <el-button link type="primary" @click.stop="copyOrderTx(row)"
                    >复制</el-button
                  >
                </div>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column label="时间" width="168">
              <template #default="{ row }">{{
                formatBeijingDateTime(row.createdAt)
              }}</template>
            </el-table-column>
            <el-table-column label="操作" width="88" fixed="right">
              <template #default="{ row }">
                <el-button
                  v-if="row.status === 'failed'"
                  link
                  type="primary"
                  @click="handleRetryOrder(row)"
                  >重试</el-button
                >
                <span v-else>-</span>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="Positions" name="positions">
          <el-table
            :data="store.tradePositions"
            size="small"
            stripe
            class="trade-table"
            table-layout="auto"
            empty-text="暂无持仓"
          >
            <el-table-column label="模式" width="92">
              <template #default="{ row }">
                <el-tag size="small" :type="modeTagType(row.tradeMode)">{{
                  tradeModeText(row.tradeMode)
                }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="84" />
            <el-table-column label="Token" min-width="180">
              <template #default="{ row }">
                <TokenAddressLink
                  :address="row.tokenAddress"
                  :short="true"
                  :compact="true"
                />
              </template>
            </el-table-column>
            <el-table-column label="数量" width="108">
              <template #default="{ row }">{{
                formatTokenAmount(row.quantity)
              }}</template>
            </el-table-column>
            <el-table-column label="成本" width="104">
              <template #default="{ row }">{{
                formatUsd(row.costAmount).replace("+", "")
              }}</template>
            </el-table-column>
            <el-table-column label="市值" width="110">
              <template #default="{ row }">{{
                formatUsd(row.marketValue).replace("+", "")
              }}</template>
            </el-table-column>
            <el-table-column label="已实现" width="110">
              <template #default="{ row }">
                <span :class="profitClass(row.realizedPnl)">{{
                  formatSignedUsd(row.realizedPnl)
                }}</span>
              </template>
            </el-table-column>
            <el-table-column label="未实现" width="110">
              <template #default="{ row }">
                <span :class="profitClass(row.unrealizedPnl)">{{
                  formatSignedUsd(row.unrealizedPnl)
                }}</span>
              </template>
            </el-table-column>
            <el-table-column label="更新时间" width="168">
              <template #default="{ row }">{{
                formatBeijingDateTime(row.updatedAt)
              }}</template>
            </el-table-column>
            <el-table-column label="操作" width="88" fixed="right">
              <template #default="{ row }">
                <el-button
                  v-if="row.status === 'open'"
                  link
                  type="danger"
                  @click="handleClosePosition(row)"
                  >平仓</el-button
                >
                <span v-else>-</span>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </section>

    <section v-if="selectedChartRange" class="panel selection-panel">
      <div>
        <strong>已选中 K 线区域</strong>
        <span
          >{{ formatShortTime(selectedChartRange.startTime) }} -
          {{ formatShortTime(selectedChartRange.endTime) }} ·
          {{ selectedChartRange.klineCount }} 根 K 线</span
        >
      </div>
      <div class="selection-actions">
        <el-button
          type="primary"
          :loading="store.loading"
          @click="loadSelectedRangeLevels"
          >计算该区域</el-button
        >
        <el-button @click="selectedChartRange = null">清除选区</el-button>
      </div>
    </section>

    <section v-if="windowOptions.length" class="panel">
      <div class="panel-heading">
        <div>
          <div class="panel-title">滑动窗口结果</div>
          <div class="panel-subtitle">
            仅保留 n 根阳线最高价触及压力带，且最后一次触及后的 n 根 K
            线内突破压力带的窗口。
          </div>
        </div>
      </div>
      <div class="window-chip-grid">
        <button
          v-for="option in windowOptions"
          :key="option.key"
          class="window-chip"
          :class="{ active: option.key === selectedWindowKey }"
          :style="{ '--window-accent': option.color }"
          @click="selectWindow(option.key)"
        >
          <strong>{{ option.label }}</strong>
          <span>{{ option.meta }}</span>
        </button>
      </div>
    </section>

    <section v-if="strategyGroups.length" class="panel strategy-panel">
      <div class="panel-heading">
        <div>
          <div class="panel-title">回测结果</div>
          <div class="panel-subtitle">
            按止盈区间分组展示；手续费按买卖合计扣减。点击某个止盈组或某笔交易，都会联动到对应
            K 线压力带。
          </div>
        </div>
      </div>
      <div class="strategy-overview">
        <div class="strategy-overview-main">
          <div class="strategy-badge">最佳组</div>
          <strong>{{ activeStrategyGroup?.label }}</strong>
          <span
            >手续费 {{ formatPercent(activeStrategyGroup?.feeRate || 0) }} ·
            交易 {{ activeStrategySummary?.tradeCount || 0 }} 笔</span
          >
        </div>
        <div class="strategy-overview-meta">
          <span
            >区间 {{ strategyForm.takeProfitRateStartPercent }}%-{{
              strategyForm.takeProfitRateEndPercent
            }}%</span
          >
          <span>步长 {{ strategyForm.takeProfitRateStepPercent }}%</span>
          <span>每笔 {{ strategyForm.positionSizeUsd }}u</span>
        </div>
      </div>
      <div class="strategy-group-grid">
        <button
          v-for="group in strategyGroups"
          :key="group.takeProfitRate"
          class="strategy-group-card"
          :class="{ active: activeStrategyGroupKey === groupKey(group) }"
          @click="selectStrategyGroup(group)"
        >
          <div class="strategy-group-head">
            <strong>{{ group.label }}</strong>
            <span :class="profitClass(group.summary.totalProfitUsd)">{{
              formatUsd(group.summary.totalProfitUsd)
            }}</span>
          </div>
          <div class="strategy-group-stats">
            <span>胜率 {{ formatPercent(group.summary.winRate) }}</span>
            <span
              >平均 {{ formatPercent(group.summary.averageProfitRate) }}</span
            >
            <span
              >回撤 {{ formatDrawdownUsd(group.summary.maxDrawdownUsd) }}</span
            >
          </div>
        </button>
      </div>
      <div class="strategy-metrics-grid compact">
        <div class="strategy-metric-card">
          <span>交易数</span
          ><strong>{{ activeStrategySummary?.tradeCount || 0 }}</strong>
        </div>
        <div class="strategy-metric-card">
          <span>胜率</span
          ><strong>{{
            formatPercent(activeStrategySummary?.winRate || 0)
          }}</strong>
        </div>
        <div class="strategy-metric-card">
          <span>净收益</span
          ><strong
            :class="profitClass(activeStrategySummary?.totalProfitUsd || 0)"
            >{{ formatUsd(activeStrategySummary?.totalProfitUsd || 0) }}</strong
          >
        </div>
        <div class="strategy-metric-card">
          <span>平均净收益率</span
          ><strong
            :class="profitClass(activeStrategySummary?.averageProfitRate || 0)"
            >{{
              formatPercent(activeStrategySummary?.averageProfitRate || 0)
            }}</strong
          >
        </div>
        <div class="strategy-metric-card">
          <span>最大回撤</span
          ><strong class="loss">{{
            formatDrawdownUsd(activeStrategySummary?.maxDrawdownUsd || 0)
          }}</strong>
        </div>
        <div class="strategy-metric-card">
          <span>总投入</span
          ><strong>{{
            formatUsd(activeStrategySummary?.committedCapitalUsd || 0)
          }}</strong>
        </div>
      </div>
      <div class="strategy-result-list">
        <button
          v-for="trade in activeStrategyTrades"
          :key="tradeKey(trade)"
          class="strategy-result-card"
          :class="{ active: isTradeFocused(trade) }"
          @click="focusTrade(trade)"
        >
          <div class="strategy-result-head">
            <div class="strategy-result-title">
              <strong
                >W{{ trade.windowIndex }} ·
                {{ trade.levelType === "support" ? "支撑" : "压力" }}
                {{ formatMarketCap(trade.levelMarketCap) }}</strong
              >
              <span class="strategy-result-hold"
                >持有 {{ trade.holdingBars }} 根</span
              >
            </div>
            <span
              class="strategy-result-rate"
              :class="profitClass(trade.profitRate)"
              >{{ formatPercent(trade.profitRate) }}</span
            >
          </div>
          <div class="strategy-result-grid">
            <div class="strategy-result-cell">
              <span class="strategy-result-label">买入</span>
              <strong
                >{{ formatShortTime(trade.buyPoint.time) }} @
                {{ formatMarketCap(trade.buyPoint.marketCap) }}</strong
              >
            </div>
            <div class="strategy-result-cell">
              <span class="strategy-result-label">卖出</span>
              <strong
                >{{ formatShortTime(trade.sellPoint.time) }} @
                {{ formatMarketCap(trade.sellPoint.marketCap) }}</strong
              >
            </div>
            <div class="strategy-result-cell">
              <span class="strategy-result-label">净收益</span>
              <strong :class="profitClass(trade.profitUsd)">{{
                formatUsd(trade.profitUsd)
              }}</strong>
            </div>
            <div class="strategy-result-cell">
              <span class="strategy-result-label">毛收益 / 手续费成本</span>
              <strong
                >{{ formatUsd(trade.grossProfitUsd) }} /
                {{ formatCostUsd(trade.feeUsd) }}</strong
              >
            </div>
            <div class="strategy-result-cell">
              <span class="strategy-result-label">净收益率</span>
              <strong :class="profitClass(trade.profitRate)">{{
                formatPercent(trade.profitRate)
              }}</strong>
            </div>
            <div class="strategy-result-cell">
              <span class="strategy-result-label">毛收益率</span>
              <strong>{{ formatPercent(trade.grossProfitRate) }}</strong>
            </div>
          </div>
          <div class="strategy-result-exit-reason">{{ trade.exitReason }}</div>
          <div class="strategy-result-meta">
            <span
              >买卖点按突破价位回测，净收益已扣除总手续费
              {{ formatPercent(trade.feeRate) }}</span
            >
          </div>
        </button>
      </div>
    </section>

    <section ref="chartPanelRef" class="panel chart-panel">
      <div class="panel-heading">
        <el-select
          v-if="levelOptions.length"
          v-model="selectedLevelKey"
          class="level-picker"
          placeholder="选择要解释的价位"
        >
          <el-option
            v-for="option in levelOptions"
            :key="option.key"
            :label="option.label"
            :value="option.key"
          />
        </el-select>
      </div>
      <KlineTradeChart
        :klines="store.result?.klines || []"
        :levels="visibleWindowLevels"
        :selected-level="selectedLevel"
        :current-window-index="selectedWindow?.windowIndex || 0"
        :window-color="selectedWindowColor"
        :strategy-trades="activeStrategyTrades"
        :focused-trade-key="focusedTradeKey"
        @range-selected="handleChartRangeSelected"
      />
    </section>

    <section v-if="sortedLevels.length" class="panel">
      <div class="panel-heading">
        <div>
          <div class="panel-title">每个支撑/压力位的计算依据</div>
          <div class="panel-subtitle">
            价格带来自滑动窗口内的局部高低点聚类；选中后可在 K
            线上看到参与计算的点位。
          </div>
        </div>
      </div>
      <div class="level-detail-grid">
        <article
          v-for="level in sortedLevels"
          :key="`${level.type}-${level.marketCap}`"
          class="level-detail-card"
          :class="[
            level.type,
            { active: levelKey(level) === selectedLevelKey },
          ]"
          :style="{ '--window-accent': selectedWindowColor }"
          @click="focusLevel(level)"
        >
          <div class="level-detail-head">
            <span class="level-badge">{{ levelDisplayName(level) }}</span>
            <strong>{{ formatMarketCap(level.marketCap) }}</strong>
          </div>
          <div class="level-band">
            {{ formatMarketCap(level.lowerMarketCap) }} -
            {{ formatMarketCap(level.upperMarketCap) }}
          </div>
          <div class="level-explain">{{ level.calculation?.typeReason }}</div>
          <dl class="level-facts">
            <div>
              <dt>Pivot</dt>
              <dd>{{ level.calculation?.pivotCount || 0 }} 个</dd>
            </div>
            <div>
              <dt>触碰</dt>
              <dd>{{ level.touches || 0 }} 次</dd>
            </div>
            <div>
              <dt>成交量</dt>
              <dd>{{ formatCompact(level.volume) }}</dd>
            </div>
            <div>
              <dt>强度</dt>
              <dd>{{ formatFixed(level.score) }}</dd>
            </div>
            <div>
              <dt>低点票</dt>
              <dd>{{ level.calculation?.supportVotes || 0 }}</dd>
            </div>
            <div>
              <dt>高点票</dt>
              <dd>{{ level.calculation?.resistanceVotes || 0 }}</dd>
            </div>
          </dl>
          <div class="score-formula">
            <span
              >强度 = 触碰
              {{ formatFixed(level.calculation?.scoreParts?.touch) }}</span
            >
            <span
              >+ 成交量
              {{ formatFixed(level.calculation?.scoreParts?.volume) }}</span
            >
            <span
              >+ 最近性
              {{ formatFixed(level.calculation?.scoreParts?.recency) }}</span
            >
            <span
              >+ 距离
              {{ formatFixed(level.calculation?.scoreParts?.distance) }}</span
            >
          </div>
          <div class="level-explain">{{ level.calculation?.statusReason }}</div>
          <div v-if="level.breakout?.consolidation" class="breakout-card">
            <strong>平台整理区</strong>
            <span
              >整理区：{{
                formatShortTime(level.breakout.consolidation.startTime)
              }}
              -
              {{ formatShortTime(level.breakout.consolidation.endTime) }}</span
            >
            <span
              >区间：{{
                formatMarketCap(level.breakout.consolidation.lowPrice)
              }}
              -
              {{
                formatMarketCap(level.breakout.consolidation.highPrice)
              }}</span
            >
            <span
              >包含 {{ level.breakout.consolidation.barCount }} 根 K 线，期间
              {{ form.minTouches }} 根阳线最高价触及压力带</span
            >
            <span v-if="level.breakout.breakoutPoint"
              >后 {{ form.minTouches }} 根内突破点：{{
                formatShortTime(level.breakout.breakoutPoint.time)
              }}
              @
              {{
                formatMarketCap(level.breakout.breakoutPoint.marketCap)
              }}</span
            >
          </div>
          <div v-else-if="level.type === 'resistance'" class="breakout-card">
            <strong>平台整理区</strong>
            <span>当前窗口下没有识别到突破前的平台整理区</span>
          </div>
        </article>
      </div>
    </section>
  </main>
</template>

<script setup>
import {
  computed,
  nextTick,
  onMounted,
  onUnmounted,
  reactive,
  ref,
  watch,
} from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { useBacktestStore } from "../stores/backtest.js";
import KlineTradeChart from "../components/KlineTradeChart.vue";
import TokenAddressLink from "../components/TokenAddressLink.vue";
import { copyText } from "../utils/clipboard.js";
import { formatBeijingDateTime, formatBeijingRFC3339 } from "../utils/time.js";

const store = useBacktestStore();
const WINDOW_COLORS = [
  "#60a5fa",
  "#f59e0b",
  "#34d399",
  "#f472b6",
  "#a78bfa",
  "#f87171",
  "#22d3ee",
  "#facc15",
];
const PRESET_TOKEN_ADDRESSES = [
  "AzdSZX3bGj2FGR8rEeH4fhrUpQhLAxvypUJMX3BDpump",
  "HKHBrJGc1Qdp2xe8X9u5ofGno5bUiZAsrn7J3mfypump",
  "cY435H7F4wcZ4NgWQFM3wUjBDffdb3qdsQST2EVpump",
  "D5MZMfvPvh21XtxntBAiL5uDE7Kag41rwNaR2Tihpump",
  "24dzjDrCfU4P28kG8VofVugMUYWZK1muHAiKxmiPpump",
  "382YojVQdcb5DV1QCj1KdN4XPVZebS22XAfDr1EMpump",
  "4McB2nCRSKQsKJUnfDTFn3wMHCC3PYuUcYAHD5taWYWG",
  "8JdjmcF8JQLPpbaiiyS6xhd5fYT2GLeNygTq8Twppump",
  "8xt6zzGFYfjqnXHaJAjMmRV98CcXJih75CPxtUVUpump",
  "5qjZLdVTe6aRtN2LWXQqnFw789HdSv99tRXXi7zYpump",
  "CtGdRWfuyRhM1VoUpPpB4KjYN39RvCBbPgPcy86ppump",
  "6MBRUitgt22meJxqpcgjLjUQbrqxuJ8RwHA14ujrpump",
  "BoYdMLSwk4WKu3CAy593hsx5ZFwY8776M8zVFvgMpump",
  "7hmTyzw374ZmB3SWxKL3Pd4ExJmLpvf4LCANbsGBpump",
  "6kKVmP1FZM7wtDNLDcHKx7zLGhA4sUt9WAo6kG9mpump",
  "AcefL9mWAzsYtRdvrbzHn8PNB9a1JVoeRXdnygAMpump",
  "6UkQu9c5t9Rj5VvSq3RCi3XYukL4RjFS951bjBK7pump",
  "3ubNQQCE1GmrAW8rA3kqWxMqGFi5HqJbmYZGL8oqpump",
  "9AdGdV46Kuuo8tAYU9g4ZFEhYtR4auVEHKj3mopapump",
  "J7nejjwUAxiSbX3DpvTkURscfFSrK33saNoU2ovHpump",
  "C86CfVQZgbQGP1NoxVYfmn2q1Mqhqv3sg6z9bYhJpump",
  "4BsYbLWnr5pHL3w3R7Z774YPwGvgC88ZtWWk76Lypump",
  "2L5aojM2AHs35aFMATM8Ju2DcNhC76aPWRV125cepump",
  "4CCiNMmqfBNRrheZKFVVdnnr4u5c9VwK5iyHxDsspump",
  "BAJtBf226aoDN5wDgJByxyspfUrxdDtqvnbSNBRupump",
  "CSzHjHF8zLTmWatEZoLYmEnoXPrGhJ8mPe52CoVmpump",
  "9Yyv1whbUfmyqFj5d5esvfmViQP8DRcDxTWTcQ3zpump",
  "HzsHZPiqizPA8jqJz2QubtwU3DiZiGf9HFyzUfQ8pump",
  "GABHHgioMnxWfnCSdubyLG4CJ4ZrHHH74Cq59XnRpump",
  "CikB2JgHPDi7ZUBBgBto865DmqwrBWNHFSng2gzTpump",
  "iaGJA8Yg2KS4Cf9JirGzXXn6pPBfVoDrSrFHXkspump",
  "qYc4gQ9xVq48XmeBBUh7GMfTYycoLS1m3VTT9tapump",
  "cbAhbADA5yxN4gbECXfhomvr4nEFpBFyFCenqNepump",
  "CPh3doM9cPhgzKSEhuRGjUf1kYdby19C9JHSANwrpump",
  "4XRCJkkqYZXhMLu2chJ2Sdw3wR6dYZTu9aTS222Kpump",
  "584zSrbS5XLnJrTe9BQMBaSvKLgvFScDxhANH1tTpump",
  "F871NCYLqnKM3mwaYnMbHrj1MUn6sAytt83Cb7yapump",
  "6L1cuzAKJ2jepALuX24iWPfQZPXWBXaWMa1jDBddpump",
  "7VjMt2Q3fsdejV28tvXBnqnhzoizSZeqwAAjZmWMpump",
  "H2cxSwXWvfHSNubCz3nTe8np2Jx9rRETYn6Cqicrpump",
  "Dig6UdRdW3PjzvR1brBXSWSoAoWALS9na9qWTwUUpump",
  "nQcXcmGN3HFMQuKszgRd4PBScC5VSS2cG16iMvypump",
  "gusmFBcXpcTd91k95abHq5CrBmkCBce92SonzxNpump",
  "Yc3WKpKKTHEtjWvzjRbFYF9VFQiPQubSw8r5gdBpump",
  "EGHexBCnfwDTAfnQD8Athwzkg4ryhd5fZELWXcf9pump",
  "adTviJVnMWtw46uBo4PQWCkCHXePJwxspojMkBDpump",
];
const form = reactive({
  tokenAddress: "cY435H7F4wcZ4NgWQFM3wUjBDffdb3qdsQST2EVpump",
  interval: "1m",
  dataSource: "gmgn",
  windowSize: 120,
  levelWindowSize: 120,
  levelWindowStep: 20,
  bandRangePercent: 0.5,
  minTouches: 3,
  confirmBars: 1,
});
const strategyForm = reactive({
  methodCode: "breakout_band_follow",
  takeProfitRateStartPercent: 8,
  takeProfitRateEndPercent: 15,
  takeProfitRateStepPercent: 1,
  feeRatePercent: 1.5,
  positionSizeUsd: 10,
  hardStopLossRatePercent: 5,
  activationProfitRatePercent: 5,
  lockedProfitRatePercent: 3,
});
const tradeTab = ref("candidates");
const tradeRuntimeMode = ref("paper");
const tradeFilterMode = ref("all");
const relativeNow = ref(Date.now());
const selectedWindowKey = ref("");
const selectedLevelKey = ref("");
const focusedTradeKey = ref("");
const activeStrategyGroupKey = ref("");
const selectedChartRange = ref(null);
const loadedRange = ref(null);
const chartPanelRef = ref(null);
const levelView = ref("resistance");
const levelViewOptions = [
  { label: "全部", value: "all" },
  { label: "支撑", value: "support" },
  { label: "压力", value: "resistance" },
];

const tokenAddressOptions = computed(() =>
  PRESET_TOKEN_ADDRESSES.map((item, index) => ({
    value: item,
    label: `#${index + 1} · ${shortAddress(item)}`,
  })),
);
const barCount = computed(() => store.result?.klines?.length || 0);
const filteredWindows = computed(() =>
  dedupeScenarioWindows(store.result?.windows || []),
);
const windowCount = computed(() => filteredWindows.value.length);
const windowOptions = computed(() =>
  filteredWindows.value.map((window, index) => ({
    key: windowKey(window),
    color: WINDOW_COLORS[index % WINDOW_COLORS.length],
    label: `W${window.windowIndex}`,
    meta: `${formatShortTime(window.startTime)} - ${formatShortTime(window.endTime)} · ${window.levels?.length || 0} 条`,
  })),
);
const selectedWindow = computed(
  () =>
    filteredWindows.value.find(
      (window) => windowKey(window) === selectedWindowKey.value,
    ) ||
    filteredWindows.value[0] ||
    null,
);
const selectedWindowColor = computed(
  () =>
    windowOptions.value.find((option) => option.key === selectedWindowKey.value)
      ?.color || WINDOW_COLORS[0],
);
const selectedWindowLevels = computed(() => selectedWindow.value?.levels || []);
const visibleWindowLevels = computed(() =>
  filterLevelsByView(selectedWindowLevels.value, levelView.value),
);
const supportCount = computed(
  () => selectedWindowLevels.value.filter(isSupportLevel).length,
);
const resistanceCount = computed(
  () => selectedWindowLevels.value.filter(isResistanceLevel).length,
);
const sortedLevels = computed(() => sortLevels(visibleWindowLevels.value));
const strategyMethodOptions = computed(() => store.strategyMethods || []);
const tradeModeOptions = computed(() =>
  store.tradeRuntime.options.length
    ? store.tradeRuntime.options.map((item) => ({
        label: item.label,
        value: item.value,
      }))
    : [
        { label: "模拟盘", value: "paper" },
        { label: "实盘", value: "live" },
      ],
);
const tradeFilterOptions = [
  { label: "全部", value: "all" },
  { label: "模拟盘", value: "paper" },
  { label: "实盘", value: "live" },
];
const dataSourceLabel = computed(() => {
  const labels = { gmgn: "GMGN", birdeye: "Birdeye", sql: "SQL", db: "DB", system: "系统K线" };
  return labels[form.dataSource] || form.dataSource || "GMGN";
});
const selectedStrategyMethod = computed(
  () =>
    strategyMethodOptions.value.find(
      (item) => item.code === strategyForm.methodCode,
    ) || null,
);
const strategyGroups = computed(
  () => store.strategyBacktestResult?.groups || [],
);
const activeStrategyGroup = computed(
  () =>
    strategyGroups.value.find(
      (group) => groupKey(group) === activeStrategyGroupKey.value,
    ) ||
    strategyGroups.value[0] ||
    null,
);
const activeStrategySummary = computed(
  () => activeStrategyGroup.value?.summary || null,
);
const activeStrategyTrades = computed(
  () => activeStrategyGroup.value?.trades || [],
);
const levelOptions = computed(() =>
  sortedLevels.value.map((level) => ({
    key: levelKey(level),
    label: `${levelDisplayName(level)} ${formatMarketCap(level.marketCap)} · 强度 ${formatFixed(level.score)}`,
  })),
);
const selectedLevel = computed(
  () =>
    sortedLevels.value.find(
      (level) => levelKey(level) === selectedLevelKey.value,
    ) ||
    sortedLevels.value[0] ||
    null,
);
const activeWindow = computed(() => {
  const range = loadedRange.value || recentFiveDayRange();
  return `${formatBeijingDateTime(range.start)} → ${formatBeijingDateTime(range.end)}`;
});
const loadedRangeLabel = computed(() => {
  if (!loadedRange.value?.source) return "";
  return `${loadedRange.value.source}：${formatShortTime(loadedRange.value.start)} - ${formatShortTime(loadedRange.value.end)}`;
});
const rangeCardTitle = computed(() =>
  loadedRange.value?.source === "选中区域" ? "选中区域" : "固定近 5 天",
);
const rangeCardDetail = computed(() => {
  const range = loadedRange.value || recentFiveDayRange();
  return `${formatShortTime(range.start)} - ${formatShortTime(range.end)}`;
});
const openTradePositions = computed(() =>
  store.tradePositions.filter((item) => item.status === "open"),
);
const closedTradePositions = computed(() =>
  store.tradePositions.filter((item) => item.status === "closed"),
);

async function loadKlineLevels() {
  selectedChartRange.value = null;
  await loadRangeLevels(recentFiveDayRange(), "近 5 天");
}

async function loadSelectedRangeLevels() {
  if (!selectedChartRange.value) {
    ElMessage.error("请先在 K 线上拖拽选择区域");
    return;
  }
  await loadRangeLevels(
    {
      start: new Date(selectedChartRange.value.startTime),
      end: new Date(selectedChartRange.value.endTime),
    },
    "选中区域",
  );
}

async function loadRangeLevels(range, sourceLabel) {
  if (!form.tokenAddress) {
    ElMessage.error("请填写 token CA");
    return;
  }
  const result = await store.loadKlineLevels({
    source: form.dataSource,
    tokenAddress: form.tokenAddress,
    interval: form.interval,
    startTime: formatBeijingRFC3339(range.start),
    endTime: formatBeijingRFC3339(range.end),
    windowSize: form.windowSize,
    levelWindowSize: form.levelWindowSize,
    levelWindowStep: form.levelWindowStep,
    priceTolerance: form.bandRangePercent / 100,
    minTouches: form.minTouches,
    confirmBars: form.confirmBars,
  });
  const firstWindow = dedupeScenarioWindows(result.windows || [])[0] || null;
  const initialLevel = sortLevels(
    filterLevelsByView(firstWindow?.levels || [], levelView.value),
  )[0];
  selectedWindowKey.value = firstWindow ? windowKey(firstWindow) : "";
  selectedLevelKey.value = initialLevel ? levelKey(initialLevel) : "";
  loadedRange.value = { ...range, source: sourceLabel };
  store.strategyBacktestResult = null;
  focusedTradeKey.value = "";
  activeStrategyGroupKey.value = "";
  ElMessage.success(
    `已加载 ${result.klines.length} 根 ${dataSourceLabel.value} K线，筛出 ${dedupeScenarioWindows(result.windows || []).length} 个压力突破场景窗口`,
  );
}

async function runStrategyForLoadedRange() {
  if (!form.tokenAddress) {
    ElMessage.error("请填写 token CA");
    return;
  }
  const range = loadedRange.value || recentFiveDayRange();
  const result = await store.runStrategyBacktest({
    dataSource: form.dataSource,
    methodCode: strategyForm.methodCode,
    methodConfig: {
      takeProfitRateStart: strategyForm.takeProfitRateStartPercent / 100,
      takeProfitRateEnd: strategyForm.takeProfitRateEndPercent / 100,
      takeProfitRateStep: strategyForm.takeProfitRateStepPercent / 100,
      positionSizeUsd: strategyForm.positionSizeUsd,
      hardStopLossRate: strategyForm.hardStopLossRatePercent / 100,
      activationProfitRate: strategyForm.activationProfitRatePercent / 100,
      lockedProfitRate: strategyForm.lockedProfitRatePercent / 100,
      feeRate: strategyForm.feeRatePercent / 100,
    },
    tokenAddress: form.tokenAddress,
    interval: form.interval,
    startTime: formatBeijingRFC3339(range.start),
    endTime: formatBeijingRFC3339(range.end),
    levelOptions: {
      windowSize: form.windowSize,
      levelWindowSize: form.levelWindowSize,
      levelWindowStep: form.levelWindowStep,
      priceTolerance: form.bandRangePercent / 100,
      minTouches: form.minTouches,
      confirmBars: form.confirmBars,
    },
  });
  const bestGroup = pickBestStrategyGroup(result.groups || []);
  activeStrategyGroupKey.value = bestGroup ? groupKey(bestGroup) : "";
  const firstTrade = bestGroup?.trades?.[0] || result.trades?.[0];
  if (firstTrade) {
    focusTrade(firstTrade);
  }
  ElMessage.success(
    `回测完成：共 ${result.groups?.length || 0} 个止盈组，最佳净收益 ${formatUsd(result.summary?.totalProfitUsd || 0)}`,
  );
}

async function loadCandidateSystemKlines(row) {
  const tokenAddress = String(row?.tokenAddress || "").trim();
  if (!tokenAddress) {
    ElMessage.error("候选项目缺少 CA");
    return;
  }
  form.tokenAddress = tokenAddress;
  const result = await store.loadRawKlines({
    source: "system",
    tokenAddress,
    interval: form.interval,
  });
  selectedChartRange.value = null;
  selectedWindowKey.value = "";
  selectedLevelKey.value = "";
  loadedRange.value = null;
  store.strategyBacktestResult = null;
  focusedTradeKey.value = "";
  activeStrategyGroupKey.value = "";
  if (!result.klines.length) {
    ElMessage.warning("该 CA 暂无系统K线");
    return;
  }
  const first = result.klines[0];
  const last = result.klines[result.klines.length - 1];
  loadedRange.value = {
    start: new Date(first.openTime),
    end: new Date(last.closeTime || last.openTime),
    source: "系统K线·全量",
  };
  ElMessage.success(`已加载 ${result.klines.length} 根系统K线`);
}

async function refreshTradeDashboard() {
  const params = {
    tradeMode: tradeFilterMode.value,
    limit: 50,
  };
  await store.loadTradeDashboard(params);
  store.startTradeStreams(params);
}

async function handleTradeModeChange(value) {
  if (value === store.tradeRuntime.tradeMode) return;
  try {
    if (value === "live") {
      await ElMessageBox.confirm(
        "切换到实盘后，后续买卖信号会真的调用 Jupiter 执行。确认继续？",
        "切换到实盘",
        {
          type: "warning",
          confirmButtonText: "确认切换",
          cancelButtonText: "取消",
        },
      );
    }
    await store.setTradeMode(value);
    tradeRuntimeMode.value = store.tradeRuntime.tradeMode;
    ElMessage.success(`已切换为${tradeModeText(store.tradeRuntime.tradeMode)}`);
    await refreshTradeDashboard();
  } catch (error) {
    tradeRuntimeMode.value = store.tradeRuntime.tradeMode;
    if (error !== "cancel" && error !== "close") {
      ElMessage.error(error.message || "切换交易模式失败");
    }
  }
}

async function handleRetryOrder(row) {
  await store.retryTradeOrder(row.id, {
    tradeMode: tradeFilterMode.value,
    limit: 50,
  });
  ElMessage.success("订单已重新提交");
}

async function handleClosePosition(row) {
  await store.closeTradePosition(row.id, {
    tradeMode: tradeFilterMode.value,
    limit: 50,
  });
  ElMessage.success("平仓指令已提交");
}

async function copyOrderTx(row) {
  const tx = String(row?.submitTxHash || "").trim();
  if (!tx) return;
  try {
    await copyText(tx);
    ElMessage.success("Tx 已复制");
  } catch (error) {
    ElMessage.error("复制失败");
  }
}

function handleChartRangeSelected(range) {
  selectedChartRange.value = range;
  ElMessage.success(
    `已选中 ${range.klineCount} 根 K 线，可点击“计算选中区域压力位”`,
  );
}

function clearSelectedRange() {
  selectedChartRange.value = null;
  loadedRange.value = null;
  loadKlineLevels();
}

function selectWindow(key) {
  selectedWindowKey.value = key;
  const targetWindow = filteredWindows.value.find(
    (window) => windowKey(window) === key,
  );
  const firstLevel = sortLevels(
    filterLevelsByView(targetWindow?.levels || [], levelView.value),
  )[0];
  selectedLevelKey.value = firstLevel ? levelKey(firstLevel) : "";
}

function focusLevel(level) {
  selectedLevelKey.value = levelKey(level);
  nextTick(() => {
    chartPanelRef.value?.scrollIntoView({ behavior: "smooth", block: "start" });
  });
}

function focusTrade(trade) {
  const window = filteredWindows.value.find(
    (item) => item.windowIndex === trade.windowIndex,
  );
  if (window) {
    selectedWindowKey.value = windowKey(window);
  }
  const level = (window?.levels || []).find(
    (item) =>
      Number(item.marketCap) === Number(trade.levelMarketCap) &&
      Number(item.lowerMarketCap) === Number(trade.levelLowerMarketCap) &&
      Number(item.upperMarketCap) === Number(trade.levelUpperMarketCap),
  );
  if (level) {
    selectedLevelKey.value = levelKey(level);
  }
  focusedTradeKey.value = tradeKey(trade);
  nextTick(() => {
    chartPanelRef.value?.scrollIntoView({ behavior: "smooth", block: "start" });
  });
}

function selectStrategyGroup(group) {
  activeStrategyGroupKey.value = groupKey(group);
  const firstTrade = group.trades?.[0];
  if (firstTrade) {
    focusTrade(firstTrade);
    return;
  }
  nextTick(() => {
    chartPanelRef.value?.scrollIntoView({ behavior: "smooth", block: "start" });
  });
}

watch(levelView, () => {
  const firstLevel = sortedLevels.value[0];
  selectedLevelKey.value = firstLevel ? levelKey(firstLevel) : "";
});

function windowKey(window) {
  return `${window.windowIndex}-${window.startTime}-${window.endTime}`;
}

function levelKey(level) {
  return `${level.type}-${Number(level.marketCap).toPrecision(12)}-${level.firstTime || ""}-${level.lastTime || ""}`;
}

function recentFiveDayRange() {
  const end = new Date();
  const start = new Date(end.getTime() - 5 * 24 * 60 * 60 * 1000);
  return { start, end };
}

function tradeKey(trade) {
  return `${trade.takeProfitRate}-${trade.windowIndex}-${trade.levelIndex}-${trade.buyPoint?.time}-${trade.sellPoint?.time}`;
}

function shortAddress(value) {
  if (!value || value.length <= 18) return value;
  return `${value.slice(0, 8)}...${value.slice(-8)}`;
}

function isTradeFocused(trade) {
  return focusedTradeKey.value === tradeKey(trade);
}

function groupKey(group) {
  return String(group.takeProfitRate);
}

function pickBestStrategyGroup(groups) {
  return (
    [...groups].sort(
      (left, right) =>
        Number(right.summary?.totalProfitUsd || 0) -
        Number(left.summary?.totalProfitUsd || 0),
    )[0] || null
  );
}

function formatMarketCap(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  if (Math.abs(number) >= 1_000_000)
    return `${(number / 1_000_000)
      .toFixed(2)
      .replace(/\.00$/, "")
      .replace(/(\.\d)0$/, "$1")}m`;
  if (Math.abs(number) >= 1_000)
    return `${(number / 1_000)
      .toFixed(2)
      .replace(/\.00$/, "")
      .replace(/(\.\d)0$/, "$1")}k`;
  return number.toFixed(2).replace(/\.00$/, "");
}

function formatOptionalMarketCap(value) {
  const number = Number(value);
  return Number.isFinite(number) && number !== 0 ? formatMarketCap(number) : "-";
}

function formatRelativeTime(value) {
  if (!value) return "-";
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return "-";
  const seconds = Math.max(0, Math.floor((relativeNow.value - timestamp) / 1000));
  if (seconds < 60) return `${seconds}s前`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h前`;
  return `${Math.floor(hours / 24)}d前`;
}

function formatFixed(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number.toFixed(2) : "0.00";
}

function formatOptionalFixed(value) {
  const number = Number(value);
  return Number.isFinite(number) && number !== 0 ? number.toFixed(2) : "-";
}

function formatPercent(value) {
  const number = Number(value);
  return Number.isFinite(number)
    ? `${(number * 100).toFixed(2).replace(/\.00$/, "")}%`
    : "-";
}

function formatUsd(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  return `${number >= 0 ? "+" : ""}${number.toFixed(2)}u`;
}

function formatSignedUsd(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  return `${number >= 0 ? "+" : ""}${number.toFixed(2)}u`;
}

function formatCostUsd(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  return `-${Math.abs(number).toFixed(2)}u`;
}

function formatDrawdownUsd(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  return `-${Math.abs(number).toFixed(2)}u`;
}

function profitClass(value) {
  return Number(value) >= 0 ? "profit" : "loss";
}

function formatTokenAmount(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "-";
  return number.toFixed(4).replace(/\.?0+$/, "");
}

function tradeModeText(value) {
  return value === "live" ? "实盘" : "模拟盘";
}

function modeTagType(value) {
  return value === "live" ? "danger" : "info";
}

function candidateStatusText(value) {
  const textMap = {
    watching: "监控中",
    bought: "已买入",
    stopped: "已停止",
    sold: "已卖出",
  };
  return textMap[value] || value || "-";
}

function candidateStatusTagType(value) {
  const typeMap = {
    watching: "warning",
    bought: "success",
    stopped: "info",
    sold: "info",
  };
  return typeMap[value] || "info";
}

function formatCompact(value) {
  const number = Number(value);
  return Number.isFinite(number)
    ? Intl.NumberFormat("zh-CN", {
        notation: "compact",
        maximumFractionDigits: 2,
      }).format(number)
    : "-";
}

function formatShortTime(value) {
  return formatBeijingDateTime(value).slice(5);
}

function sortLevels(levels) {
  return [...levels].sort((left, right) => {
    const leftName = levelDisplayName(left);
    const rightName = levelDisplayName(right);
    if (leftName !== rightName) return leftName === "支撑" ? -1 : 1;
    return Number(right.score || 0) - Number(left.score || 0);
  });
}

function filterLevelsByView(levels, view) {
  if (view === "all") return levels;
  if (view === "support") return levels.filter(isSupportLevel);
  if (view === "resistance") return levels.filter(isResistanceLevel);
  return levels;
}

function dedupeScenarioWindows(windows) {
  const seenSignatures = new Set();
  const deduped = [];
  for (const window of windows) {
    const uniqueLevels = [];
    let hasNewScenario = false;
    for (const level of window.levels || []) {
      if (!isBreakoutResistanceLevel(level)) {
        uniqueLevels.push(level);
        continue;
      }
      const signature = levelScenarioSignature(level);
      if (!signature || seenSignatures.has(signature)) {
        continue;
      }
      seenSignatures.add(signature);
      uniqueLevels.push(level);
      hasNewScenario = true;
    }
    if (hasNewScenario) {
      deduped.push({ ...window, levels: uniqueLevels });
    }
  }
  return deduped;
}

function levelScenarioSignature(level) {
  const touches = level?.breakout?.failedTouches || [];
  const breakoutTime = level?.breakout?.breakoutPoint?.time;
  if (!touches.length || !breakoutTime) return "";
  return `pressure:${touches.map((point) => point.time).join("|")}|breakout:${breakoutTime}`;
}

function isBreakoutResistanceLevel(level) {
  return (
    isResistanceLevel(level) &&
    level.breakout?.consolidation &&
    level.breakout?.breakoutPoint
  );
}

function isSupportLevel(level) {
  return (
    Number(level?.calculation?.supportVotes || 0) > 0 ||
    level?.type === "support"
  );
}

function isResistanceLevel(level) {
  return (
    Number(level?.calculation?.resistanceVotes || 0) > 0 ||
    level?.type === "resistance"
  );
}

function levelDisplayName(level) {
  if (isResistanceLevel(level) && level?.breakout?.breakoutPoint) return "压力";
  return level?.type === "support" ? "支撑" : "压力";
}

let relativeTimer = null;

onMounted(async () => {
  relativeTimer = window.setInterval(() => {
    relativeNow.value = Date.now();
  }, 1000);
  await Promise.all([store.loadStrategyMethods(), store.loadTradeRuntime()]);
  tradeRuntimeMode.value = store.tradeRuntime.tradeMode || "paper";
  await refreshTradeDashboard();
});

onUnmounted(() => {
  if (relativeTimer) {
    window.clearInterval(relativeTimer);
  }
  store.stopTradeStreams();
});
</script>

<style scoped>
.trade-panel {
  display: grid;
  gap: 12px;
}

.trade-panel-heading {
  align-items: flex-start;
}

.trade-runtime-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.trade-runtime-card {
  display: grid;
  gap: 8px;
  padding: 12px 14px;
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 12px;
  background: rgba(15, 23, 42, 0.26);
}

.trade-runtime-label {
  color: #d8e9e2;
  font-size: 12px;
  font-weight: 700;
}

.trade-runtime-hint {
  color: rgba(216, 233, 226, 0.72);
  font-size: 12px;
  line-height: 1.5;
}

.trade-kpis {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.trade-kpi {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border-radius: 12px;
  background: linear-gradient(
    180deg,
    rgba(13, 20, 35, 0.88),
    rgba(17, 24, 39, 0.74)
  );
  border: 1px solid rgba(96, 165, 250, 0.14);
}

.trade-kpi span {
  color: rgba(216, 233, 226, 0.65);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.trade-kpi strong {
  color: #f8fafc;
  font-size: 18px;
}

.trade-tabs :deep(.el-tabs__header) {
  margin-bottom: 10px;
}

.trade-tabs :deep(.el-tabs__nav-wrap::after) {
  background: rgba(148, 163, 184, 0.18);
}

.trade-table :deep(.el-table__cell) {
  padding: 8px 0;
}

.tx-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.tx-cell :deep(.el-button) {
  margin-left: 0;
}

.trade-cell-stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.trade-cell-stack strong {
  color: #0f172a;
  font-size: 12px;
}

.candidate-symbol-link {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  padding: 0;
  border: 0;
  background: transparent;
  color: #0f172a;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}

.candidate-symbol-link:hover {
  color: #2563eb;
}

.trade-cell-stack span {
  color: #64748b;
  font-size: 11px;
}

.breakout-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 12px;
  padding: 12px;
  border: 1px solid rgba(96, 165, 250, 0.18);
  border-radius: 12px;
  background: rgba(15, 23, 42, 0.28);
}

.selection-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.selection-panel > div:first-child {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.selection-panel span {
  color: rgba(216, 233, 226, 0.72);
}

.selection-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.token-ca-select {
  width: 100%;
}

.strategy-group-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.strategy-group-title-note {
  color: #94a3b8;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.strategy-config-panel {
  display: grid;
  gap: 10px;
  padding: 12px;
  border: 1px solid #dbe4f0;
  border-radius: 12px;
  background: linear-gradient(
    180deg,
    rgba(248, 250, 252, 0.96),
    rgba(241, 245, 249, 0.88)
  );
}

.strategy-topline {
  display: grid;
  grid-template-columns: minmax(220px, 300px) minmax(0, 1fr);
  gap: 10px;
  align-items: stretch;
}

.strategy-method-select {
  min-width: 0;
}

.strategy-overview-chips {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.strategy-overview-chip,
.strategy-field-section {
  border: 1px solid #d7e0eb;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.86);
}

.strategy-overview-chip {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  min-height: 70px;
  padding: 12px 14px;
}

.strategy-overview-chip span,
.strategy-section-head span {
  color: #64748b;
  font-size: 12px;
}

.strategy-overview-chip strong {
  color: #0f172a;
  font-size: 16px;
  line-height: 1.25;
}

.strategy-overview-chip em {
  color: #475569;
  font-size: 12px;
  font-style: normal;
}

.strategy-section-grid {
  display: grid;
  gap: 10px;
}

.strategy-field-section {
  padding: 12px;
}

.strategy-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.strategy-section-head strong {
  color: #0f172a;
  font-size: 14px;
}

.strategy-field-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.strategy-field-grid-risk {
  grid-template-columns: repeat(5, minmax(0, 1fr));
}

.strategy-caption {
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.78);
  color: #52627a;
  font-size: 13px;
  line-height: 1.5;
}

@media (max-width: 1100px) {
  .trade-runtime-grid,
  .trade-kpis {
    grid-template-columns: 1fr;
  }
}

.strategy-panel {
  padding: 16px;
}

.strategy-overview {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  margin-bottom: 14px;
  padding: 14px 16px;
  border: 1px solid rgba(143, 178, 168, 0.14);
  border-radius: 14px;
  background:
    linear-gradient(135deg, rgba(8, 26, 31, 0.92), rgba(16, 42, 36, 0.74)),
    radial-gradient(
      circle at top right,
      rgba(245, 158, 11, 0.18),
      transparent 14rem
    );
}

.strategy-overview-main,
.strategy-overview-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.strategy-overview-main strong {
  font-size: 20px;
}

.strategy-overview-main span,
.strategy-overview-meta span {
  color: rgba(216, 233, 226, 0.74);
  font-size: 13px;
}

.strategy-badge {
  display: inline-flex;
  align-self: flex-start;
  padding: 4px 10px;
  border-radius: 999px;
  background: rgba(250, 204, 21, 0.14);
  color: #fde68a;
  font-size: 12px;
  font-weight: 700;
}

.strategy-group-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}

.strategy-group-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  color: inherit;
  text-align: left;
  border: 1px solid rgba(143, 178, 168, 0.16);
  border-radius: 12px;
  background: rgba(8, 22, 28, 0.72);
  cursor: pointer;
}

.strategy-group-card.active {
  border-color: rgba(52, 211, 153, 0.72);
  box-shadow: 0 0 0 1px rgba(52, 211, 153, 0.28);
  background: rgba(8, 28, 24, 0.82);
}

.strategy-group-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
}

.strategy-group-stats {
  display: grid;
  gap: 4px;
  color: rgba(216, 233, 226, 0.72);
  font-size: 12px;
}

.strategy-metrics-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}

.strategy-metrics-grid.compact {
  margin-bottom: 12px;
}

.strategy-metric-card {
  padding: 12px;
  border: 1px solid rgba(143, 178, 168, 0.16);
  border-radius: 12px;
  background: rgba(10, 25, 31, 0.72);
}

.strategy-metric-card span {
  display: block;
  color: rgba(216, 233, 226, 0.68);
  font-size: 12px;
}

.strategy-metric-card strong {
  display: block;
  margin-top: 8px;
  font-size: 18px;
}

.strategy-result-list {
  display: grid;
  gap: 6px;
}

.strategy-result-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  color: inherit;
  text-align: left;
  border: 1px solid rgba(143, 178, 168, 0.16);
  border-radius: 10px;
  background: rgba(10, 25, 31, 0.72);
  cursor: pointer;
}

.strategy-result-card.active {
  border-color: rgba(96, 165, 250, 0.8);
  box-shadow: 0 0 0 1px rgba(96, 165, 250, 0.35);
}

.strategy-result-head {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  align-items: flex-start;
}

.strategy-result-title {
  display: grid;
  gap: 0;
}

.strategy-result-title strong {
  font-size: 14px;
  line-height: 1.25;
}

.strategy-result-hold {
  color: rgba(216, 233, 226, 0.54);
  font-size: 11px;
  line-height: 1.2;
}

.strategy-result-rate {
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 700;
  line-height: 1.2;
}

.strategy-result-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px 10px;
}

.strategy-result-cell {
  display: grid;
  gap: 1px;
  min-width: 0;
}

.strategy-result-label {
  color: rgba(216, 233, 226, 0.54);
  font-size: 10px;
  line-height: 1.1;
  letter-spacing: 0.02em;
}

.strategy-result-cell strong {
  font-size: 12px;
  line-height: 1.25;
  color: rgba(240, 248, 244, 0.92);
  word-break: break-word;
}

.strategy-result-exit-reason {
  padding: 4px 6px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.04);
  color: rgba(228, 239, 235, 0.8);
  font-size: 11px;
  line-height: 1.3;
}

.strategy-result-meta {
  color: rgba(216, 233, 226, 0.58);
  font-size: 10px;
  line-height: 1.25;
}

@media (max-width: 960px) {
  .strategy-topline,
  .strategy-overview-chips,
  .strategy-field-grid,
  .strategy-field-grid-risk {
    grid-template-columns: 1fr;
  }
  .strategy-section-head {
    flex-direction: column;
    align-items: flex-start;
  }
  .strategy-overview {
    flex-direction: column;
    align-items: flex-start;
  }
  .strategy-group-grid,
  .strategy-metrics-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .strategy-result-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .strategy-result-head {
    flex-direction: column;
    gap: 4px;
  }
  .strategy-result-grid {
    grid-template-columns: 1fr;
  }
}
</style>
