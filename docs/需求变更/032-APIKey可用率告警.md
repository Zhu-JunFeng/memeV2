# API Key 可用率告警

## 需求

- 普通 API 限流属于可恢复的正常情况，不再发送 Telegram 告警。
- 只有 GMGN API Key 池整体可用率严格低于 `50%` 时，才发送 API Key 告警；Birdeye Key 池不参与告警。

## 规则

- 每 `30s` 只查询 `gmgn_api_keys`。
- 可用率按 `status = available` 的数量除以 Key 总数计算。
- 可用率 `< 50%` 时发送“API Key 可用率过低”告警；可用率 `= 50%` 时不告警。
- Key 池没有记录时不计算可用率，也不发送告警。
- 告警内容包含 Key 池名称、可用数量、总数量和可用率，不包含 API Key 原文。
- GMGN Key 池告警后沿用 `alert.cooldown_seconds` 冷却时间；恢复到 `>= 50%` 并连续两次检查正常后发送恢复通知。
- `birdeye_api_keys` 不读取、不计算可用率，也不发送 TG 可用率告警。

## 不再告警的情况

- HTTP 或业务码 `429`：仍按既有逻辑冷却当前 Key、轮换其他 Key，但不发送 TG。
- 单次 `401/403`：不直接发送 TG，以数据库 Key 池整体状态为准。
- `401/403/429` 响应即使耗时超过延迟阈值，也不产生高延迟类别的重复告警。

## 保留的其他运行告警

- 网络错误和其他 HTTP 失败连续达到阈值。
- 外部请求连续超过延迟阈值。
- 服务器 CPU 或内存连续超过资源阈值。

## 配置变化

- 删除不再生效的 `alert.consecutive_rate_limits` 配置。
- Key 池检查复用 `alert.resource_check_interval_seconds`，生产默认每 `30s` 执行一次。
