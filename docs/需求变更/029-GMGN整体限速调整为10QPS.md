# GMGN 整体限速调整为 10 QPS

## 变更内容

- 所有 GMGN 调用共享同一个全局 `10 QPS` 限速器，不再按 API Key 分别计算 QPS。
- GMGN K 线、报价、项目发现和 token symbol 查询共同占用整体 `10 QPS`。
- API Key 仍从 PostgreSQL `gmgn_api_keys` 表轮询选取，不恢复固定 Key。
- 单个 Key 收到 HTTP 或业务码 `429` 后仍冷却 `60s`，池中其他可用 Key 可以继续参与轮询。

## 吞吐语义

- 无论数据库中有多少个 `available` Key，进程内 GMGN 理论请求上限均为整体 `10 QPS`。
- 候选池的 16 个 worker 只负责并发处理，不会把 GMGN 请求速率叠加到 `Key 数量 x 10 QPS`。
- Key 池不可用时直接返回错误，不增加备用数据源或降级逻辑。
