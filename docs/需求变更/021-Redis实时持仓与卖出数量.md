# Redis 实时持仓与卖出数量

## 目标

- 买入成交后，把全部未平仓持仓快照写入 Redis。
- 最新价格、浮动盈亏和历史最大收益更新时，同步刷新 Redis 快照。
- 自动卖出和手动平仓均使用 Redis 中的最新持仓数量。
- Redis 持仓 Hash 不设置过期时间。

## Key 结构

- Key：`solana_meme_v2:trade:open_positions:<accountId>`
- 类型：Hash
- Field：Token CA
- Value：完整 `TradePosition` JSON

## 生命周期

- 服务启动：从 PostgreSQL 查询未平仓记录并回填 Redis，用于兼容已有仓位和灾难恢复。
- 买入成功：写入 Redis，并保留数据库订单、成交和持仓记录。
- 价格刷新：同时更新 PostgreSQL、进程内缓存和 Redis。
- 卖出信号：从 Redis 读取仓位和数量后提交卖单。
- 卖出失败：恢复进程内缓存并保留 Redis 仓位。
- 卖出成功：删除 Redis Hash 中对应 CA 的字段。

PostgreSQL 继续承担交易审计与启动恢复，Redis 是运行时卖出数量的数据源。
