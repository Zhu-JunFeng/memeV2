# 候选池 Redis 订阅适配

## 背景

老版 solana-scalper 在项目进入候选池且评分合格后，会通过 Redis Pub/Sub 广播候选池消息。V2 交易模块原先只消费本系统压力带突破信号，因此存在两个不匹配点：

- 订阅通道不同：老版广播通道为 `solana_scalper:candidate_pool`，V2 默认通道为 `solana:meme:signals:pressure_breakout`。
- 消息结构不同：老版消息事件为 `candidate_score_passed`，V2 原生交易消息结构为 `TradeSignalMessage`。

## 变更内容

- Redis 配置新增 `redis.consumer_channel`：
  - 仅控制交易模块订阅通道。
  - 为空时沿用 `redis.channel`，保持原有压力带突破信号发布与消费方式不变。
  - 对接老版候选池时配置为 `solana_scalper:candidate_pool`。
- 交易模块新增 `candidate_score_passed` 消息适配：
  - 转换为 V2 内部买入信号。
  - `signalType` 固定为 `buy`。
  - `strategyCode` 固定为 `candidate_score_passed`。
  - `interval` 固定为 `candidate_pool`。
  - `signalTime` 使用消息中的 `publishedAt` 毫秒时间戳。
  - `triggerPrice` 使用 `signalPrice`。
  - `triggerMarketCap` 使用 `marketCap`。
  - 原始候选池消息保存在 `metadata` 中。
- 交易 Worker 启动后会等待 Redis subscribe ack，并输出订阅成功或失败日志，便于线上确认。

## 生产配置示例

```yaml
redis:
  enabled: true
  addr: "182.92.160.46:6379"
  password: "root"
  db: 0
  channel: "solana:meme:signals:pressure_breakout"
  consumer_channel: "solana_scalper:candidate_pool"

trade:
  enabled: true
  signal_consumer: true
```

## 消费后的业务语义

候选池评分合格消息进入 V2 后，按一次买入信号处理。后续是否真实下单仍由 V2 当前交易模式控制：

- `paper`：记录信号、订单和模拟成交。
- `live`：走 Jupiter 真实下单链路。

同一个候选池消息会生成确定性的 `signalId`，用于避免重复消费导致重复建单。
