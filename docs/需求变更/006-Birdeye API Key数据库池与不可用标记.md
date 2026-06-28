# Birdeye API Key 数据库池与不可用标记

## 背景

候选池二次监控需要持续调用 Birdeye 最新 K 线。如果配置文件里的 key 出现 `Compute units usage limit exceeded`，原先只能依赖进程内 key 池继续轮换，无法在数据库中记录哪个 key 已不可用，也无法在线新增 key。

## 业务规则

- 新增 PostgreSQL 表 `birdeye_api_keys` 保存 Birdeye API Key 池。
- 服务启动时会把配置文件 `birdeye.api_keys` / `birdeye.api_key` 中的 key 导入数据库；已存在的 key 不会被启动流程重置状态。
- 所有直接调用 Birdeye 的路径默认从数据库读取 `available` 状态 key，并按轮询方式选择 key。
- 某个 key 调用失败后，会继续尝试数据库中其他可用 key，直到本次读取到的可用 key 都尝试完。
- 如果 Birdeye 返回 `Compute units usage limit exceeded`，当前使用的 key 会被标记为 `unavailable`，并记录不可用原因和时间。
- 新增接口 `POST /api/birdeye/api-keys` 用于在线新增 key；如果 key 已存在，该接口会把它重新标记为 `available`。

## 数据范围

当前接入数据库 key 池的 Birdeye 调用包括：

- Birdeye K 线与市值数据源。
- Birdeye 钱包交易点数据源。
- 候选池二次监控使用的最新 K 线查询。

## 表注释

本次同时在 PostgreSQL 迁移中补充所有现有业务表和字段的中文注释，便于后续直接在数据库元数据中查看字段含义。
