# XXYY 候选项目接入

## 目标

V2 使用 XXYY `NEW` feed 获取候选项目，每 60 秒执行一次。当返回项目的 KOL 数量大于等于 5 时，将其 CA 加入现有候选池。

## 实现约束

- 接口：`POST /api/trade/open/api/feed/NEW?chain=sol`。
- 鉴权：XXYY API Key 规范化为 `Bearer xxyy_ak_...` 后写入 `Authorization` 请求头；配置值已带 `Bearer` 时不重复添加。
- CA 优先读取 `ca`，缺失时读取 `tokenAddress`。
- KOL 优先读取 `kolNum`，缺失时读取 `kol`。
- 请求体携带 `kol: "5,"`，代码仍再次执行 `kol >= 5` 的过滤；`kol == 5` 可以进入候选池。
- 单次响应内按 CA 去重，再调用现有 `CandidateMonitor.AddManualCandidate`，沿用 Redis 活动候选去重和后续监控流程。
- XXYY 请求失败只记录错误，不调用其他项目来源。
- 服务启动后立即拉取一次，之后每分钟拉取一次。

## 配置

配置位于 `xxyy` 节点。生产环境启用 `enabled` 并配置 `api_key`；真实 Key 不进入代码仓库。环境变量分别为 `BACKTEST_XXYY_ENABLED`、`BACKTEST_XXYY_API_KEY`、`BACKTEST_XXYY_BASE_URL` 和 `BACKTEST_XXYY_POLL_INTERVAL_SECONDS`。
