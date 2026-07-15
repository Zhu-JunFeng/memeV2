# GMGN 每 Key 独立限速与统一 Key 池轮询

## 变更内容

- `gmgn.max_qps` 从进程级全局限速改为单个 API Key 的独立限速，默认每 Key `8 QPS`。
- 所有 GMGN 调用统一从 PostgreSQL `gmgn_api_keys` 表读取状态为 `available` 的 Key。
- GMGN K 线、报价、项目发现和 token symbol 查询共享同一个进程内调度器和轮询游标，不再固定使用配置中的第一个 Key。
- 单个 Key 被 GMGN 返回 HTTP 或业务码 `429` 时，本次请求继续尝试池中的下一个 Key。
- 收到 `429` 的 Key 进入 `60s` 冷却，冷却期间不再发请求，避免持续请求延长上游临时封禁；其他可用 Key 继续轮询。
- 请求成功后沿用现有逻辑更新该 Key 的最近成功时间。

## 吞吐语义

- 每个 Key 单独维护请求间隔，不再由一个全局限速器串行阻塞。
- 2 个可用 Key 且每 Key `8 QPS` 时，理论总吞吐约为 `16 QPS`。
- K 线与项目源共享额度统计，避免同一个 Key 被不同调用模块各自按 `8 QPS` 重复放量。

## 数据源约束

- 配置中的 `gmgn.api_key` / `gmgn.api_keys` 仅用于启动时补充数据库 Key 池。
- 生产请求只使用数据库中状态为 `available` 的 GMGN Key。
- Key 池为空或读取失败时直接返回错误，不增加备用数据源或降级逻辑。
