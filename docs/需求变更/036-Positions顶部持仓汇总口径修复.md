# Positions 顶部持仓汇总口径修复

## 问题

Positions 顶部的 `Open 持仓` 数量只统计 `status=open`，但持仓成本、当前价值和总收益原先对当前列表中的 open、closed 记录全部求和，造成数量与金额口径不一致。

## 修复

- `Open 持仓`、持仓成本、当前价值和总收益统一只统计 `status=open` 的当前持仓。
- closed 历史记录继续保留在表格中展示，不再计入顶部当前持仓汇总。
- open 持仓缺少正数 `marketValue` 时，继续按 `costAmount + realizedPnl + unrealizedPnl` 计算展示价值。

## 验证

- 一个 open 持仓和多个 closed 历史持仓同时存在时，顶部金额只等于该 open 持仓。
- closed 记录的已实现盈亏不会混入顶部当前持仓总收益。
