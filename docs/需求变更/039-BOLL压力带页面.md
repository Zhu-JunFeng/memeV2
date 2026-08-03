# 039-BOLL 压力带页面

## 背景

需要一个独立的小页面，用当前系统 K 线数据验证指定 CA 的 BOLL 压力带，并把全部动态压力带直接叠加到 K 线图上，便于观察价格触及上轨和突破上轨的位置。

默认 CA：`7V6Sk63y8Rr1MvcN5mYNp61wgFhy4EeQg5gUASk9pump`。

## 页面入口

- 路由：`/bollinger-pressure`
- 主导航新增：`BOLL 压力带`

## 数据来源

页面默认调用现有接口：

- `GET /api/market/klines`
- 默认参数：
  - `source=system`
  - `tokenAddress=7V6Sk63y8Rr1MvcN5mYNp61wgFhy4EeQg5gUASk9pump`
  - `interval=1m`

页面不添加备用数据源逻辑；用户选择哪个数据源，就只使用该数据源返回的 K 线。

## BOLL 计算口径

前端使用 pandas_ta `bbands` 等价公式计算：

```text
MIDDLE = SMA(close, length)
STDDEV = rolling_std(close, length, ddof)
UPPER = MIDDLE + std * STDDEV
LOWER = MIDDLE - std * STDDEV
BANDWIDTH = 100 * (UPPER - LOWER) / MIDDLE
PERCENT = (close - LOWER) / (UPPER - LOWER)
```

默认参数：

- `length=20`
- `std=2`
- `ddof=0`
- `close` 使用 K 线的 `marketCapClose`

## 压力带展示

- K 线使用市值 OHLC：`marketCapOpen/high/low/close`。
- 红色半透明区域表示每一根有效 K 线的 BOLL `middle -> upper` 区间，即动态压力带。
- 橙色线：BOLL 上轨，也就是压力带上沿。
- 灰色线：BOLL 中轨，也就是压力带下沿。
- 绿色线：BOLL 下轨。
- 黄色圆点：最高价触及上轨但收盘没有站上上轨。
- 蓝色圆点：收盘价突破上轨。

## 本次变更文件

- `frontend/src/views/BollingerPressurePage.vue`
- `frontend/src/utils/bollinger.js`
- `frontend/src/utils/bollinger.test.js`
- `frontend/src/router/index.js`
- `frontend/src/App.vue`
- `frontend/src/styles.css`

## B/S 策略标记

页面新增基于当前 `breakout_band_follow` 规则的 B/S 模拟标记，但压力带来源改为 BOLL：

- 触压：阳线最高价落在 BOLL `middle -> upper` 压力带内，并满足当前策略的相对放量要求。
- 压力冻结：满足第 `n` 次触压后，冻结该触压 K 线对应的 BOLL `middle` 和 `upper`。
- 买入 B：后续 `n` 根 K 线内，收盘价突破冻结上轨 `upper * (1 + 1%)`。
- 卖出 S：沿用当前策略卖出顺序：动态止损、固定 `5%` 止损、固定 `8%` 止盈；如果样本结束仍未触发，则按最后一根 K 线收盘卖出。
- 手续费：净收益按当前默认总手续费 `1.5%` 扣除。

图上标记：

- `B`：BOLL 压力突破买入点。
- `S`：按当前卖出策略得到的卖出点，盈利为蓝色，亏损为红色。

## 加载性能

- 页面默认 `回看小时=0`，不传时间范围，按接口加载全部 K 线。
- 如果全量 1m K 线响应过慢，可以手动把 `回看小时` 调大为具体小时数，例如 `3` 或 `24`。
