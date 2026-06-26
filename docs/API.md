# API 文档

所有接口返回统一结构：

```json
{
  "code": 0,
  "message": "成功",
  "data": {},
  "traceId": "..."
}
```

时间约定：

- 所有接口入参和返回值统一使用北京时间（UTC+08:00）。
- 时间入参优先使用 RFC3339，例如 `2026-06-22T08:00:00+08:00`。
- 对不带时区的时间字符串，例如 `2026-06-22T08:00:00`，后端按北京时间解析。

## 健康检查

### GET /api/health

返回服务状态。

## Token 搜索

### GET /api/tokens/search

参数：

- `keyword`：搜索关键词。

返回：

- `items`：token 列表，字段包含 `address`、`symbol`、`name`。

说明：该接口依赖 `BACKTEST_DATASOURCE_TOKEN_SEARCH_QUERY` 或配置文件 `datasource.token_search_query`。

## K 线查询

### GET /api/market/klines

参数：

- `tokenAddress`：token 地址。
- `interval`：K 线粒度，例如 `1m`、`5m`、`15m`、`1h`。
- `startTime`：北京时间 RFC3339 开始时间。
- `endTime`：北京时间 RFC3339 结束时间。
- `source`：可选，`sql` 或 `birdeye`；不传时使用 SQL 数据源。

返回：

- `items`：K 线数组，字段包含 `openTime`、`closeTime`、`marketCapOpen`、`marketCapHigh`、`marketCapLow`、`marketCapClose`、`volume`。

说明：SQL 数据源依赖 `BACKTEST_DATASOURCE_KLINE_QUERY` 或配置文件 `datasource.kline_query`。SQL 参数顺序固定为 `tokenAddress`、`interval`、`startTime`、`endTime`。

### GET /api/market/birdeye/klines

Birdeye K 线专用入口。参数同 `/api/market/klines`，但固定使用 Birdeye 数据源。

配置：

- `BACKTEST_BIRDEYE_API_KEY`：Birdeye API Key，必填。
- `BACKTEST_BIRDEYE_API_KEYS`：可选，Birdeye API Key 池，支持逗号分隔多个 key；当某个 key 遇到 `429` 或额度耗尽时，后端会自动切换到下一个 key。
- `BACKTEST_BIRDEYE_BASE_URL`：默认 `https://public-api.birdeye.so`。
- `BACKTEST_BIRDEYE_CHAIN`：默认 `solana`。
- `BACKTEST_DATABASE_DSN`：PostgreSQL 连接串；Birdeye K 线缓存、回测结果、交易模块表统一存储在同一个 PG 库。

说明：未配置 API Key 时直接返回中文错误，不改用 SQL 或其他数据源。Birdeye K 线首次拉取成功后会写入 PostgreSQL cache；同一个 token + interval 只要本地已经缓存过，后续都直接优先读取该项目缓存，不再为了追最新 K 线重复请求 Birdeye。


### GET /api/market/birdeye/support-resistance

根据 CA 获取 Birdeye 市值 K 线，并基于这批市值 K 线计算支撑位、压力位。

参数：

- `tokenAddress`：token CA。
- `interval`：K 线粒度，例如 `1m`、`5m`、`15m`、`1h`。
- `startTime`：北京时间 RFC3339 开始时间；当前前端固定传近 5 天开始时间。
- `endTime`：北京时间 RFC3339 结束时间；当前前端固定传当前时间。
- `windowSize`：按多少根 K 线组成一个计算窗口；当前前端可配置。
- `pivotWindow`：可选，局部高低点窗口，默认 `5`。
- `priceTolerance`：可选，合并相近市值位的基础容忍度，默认 `0.02`；当前前端用“带宽范围(%)”传入。
- `breakTolerance`：可选，突破/跌破确认容忍度，默认 `0.01`。
- `confirmBars`：可选，连续确认 K 线数量，默认 `2`。
- `volumeWindow`：可选，成交量确认窗口，默认 `20`。
- `volumeMultiplier`：可选，放量突破倍数，默认 `1.2`。
- `maxLevels`：可选，每类最多返回多少个强支撑/压力位，默认 `8`。
- `minTouches`：可选，定义“多次试压”最少需要几次触及压力带，默认 `3`；每根试压阳线除满足阳线且最高点进入压力带外，还必须满足更高的成交量门槛，当前按最近 `volumeWindow` 根均量的至少 `1.35x` 与 `volumeMultiplier` 中较高者执行。
- `entryOffsetBars`：可选，突破后延迟多少根 K 线再买入，默认 `1`，即下一根开盘买入。
- `takeProfitRR`：可选，止盈盈亏比，默认 `2`。
- `maxHoldBars`：可选，买入后最多持有多少根 K 线，默认 `30`。

返回：

- `klines`：Birdeye 市值 K 线数组，字段包含 `marketCapOpen`、`marketCapHigh`、`marketCapLow`、`marketCapClose`。
- `windowSize`：实际生效的窗口 K 线数量。
- `windowStep`：窗口滑动步长，当前固定为 `1`。
- `windows`：滑动窗口结果数组。
- `windows[].startTime/endTime`：该窗口覆盖的 K 线时间范围。
- `windows[].klineCount`：该窗口包含的 K 线数量。
- `windows[].levels`：该窗口下的支撑/压力位数组，字段包含 `marketCap`、`lowerMarketCap`、`upperMarketCap`。
- `windows[].levels[].calculation`：该市值位的计算依据，包含 pivot 数量、支撑/压力票数、价格容忍度、当前市值、类型判定原因、状态判定原因、强度分拆解、形成市值位的局部高低点和触碰样本。
- `windows[].levels[].breakout`：压力位突破回测结果；会返回试压点 `failedTouches`、突破点 `breakoutPoint`、买入点 `buyPoint`、止损止盈价格、退出点、收益率，以及最终是先止损、先止盈还是超时平仓。当前场景还要求：试压组与突破点之间，最多只允许 `1` 根 K 线的最高价刺穿压力带上沿，超过则不算有效场景。
- 在试压组内部，从第一根试压阳线到最后一根试压阳线之间，若收盘价高于压力带上沿的 K 线超过 `3` 根，该场景直接过滤，不进入回测。

说明：未配置 Birdeye API Key 时直接返回中文错误，不改用 SQL、DB 或其他数据源。接口内部会先查 PostgreSQL cache；同一个 token + interval 只要已经缓存过，后续都直接复用该项目缓存，不再请求 Birdeye 最新 K 线。只有首次没有任何缓存时才会调用 Birdeye，并把成功返回的 K 线写入 PG。

### GET /api/strategy-backtests/methods

返回可用回测方法列表，供前端按方法编码和参数定义渲染配置表单。

返回：

- `items[].code`：方法编码。
- `items[].name`：方法名称。
- `items[].description`：方法说明。
- `items[].params`：参数定义，当前包含 `key`、`label`、`description`、`type`、`defaultValue`、`minValue`、`maxValue`、`step`。

### POST /api/strategy-backtests/run

按指定 CA、区间和回测方法执行策略回测。当前固定使用 Birdeye K 线，并优先复用 PostgreSQL cache 中已覆盖的 K 线。

请求体：

```json
{
  "methodCode": "breakout_band_follow",
  "methodConfig": {
    "takeProfitRateStart": 0.08,
    "takeProfitRateEnd": 0.15,
    "takeProfitRateStep": 0.01,
    "positionSizeUsd": 10,
    "hardStopLossRate": 0.05,
    "activationProfitRate": 0.05,
    "lockedProfitRate": 0.03,
    "feeRate": 0.015
  },
  "tokenAddress": "token address",
  "interval": "1m",
  "startTime": "2026-06-25T08:00:00+08:00",
  "endTime": "2026-06-30T08:00:00+08:00",
  "levelOptions": {
    "windowSize": 120,
    "levelWindowSize": 120,
    "levelWindowStep": 20,
    "priceTolerance": 0.005,
    "minTouches": 3,
    "confirmBars": 1
  }
}
```

返回：

- `methodCode` / `methodName`：本次使用的回测方法。
- `summary`：当前回测里净收益最好的止盈组统计。
- `groups`：按止盈比例分组的结果数组。
- `groups[].takeProfitRate`：该组止盈比例。
- `groups[].feeRate`：该组使用的总手续费比例。
- `groups[].summary`：该止盈组的聚合统计，已扣除手续费。
- `groups[].trades`：该止盈组下的逐笔交易结果。
- `trades`：兼容字段，等于最佳止盈组的逐笔交易结果。
- `trades[].windowIndex`：信号来自哪个滑动窗口。
- `trades[].levelMarketCap/lowerMarketCap/upperMarketCap`：触发买入的压力带。
- `trades[].buyPoint/sellPoint`：买卖点时间与市值。
- `trades[].profitRate/profitUsd`：扣手续费后的净收益率与净收益。
- `trades[].grossProfitRate/grossProfitUsd`：扣手续费前的毛收益率与毛收益。
- `trades[].feeRate/feeUsd`：本笔按固定投入折算的总手续费比例与手续费金额。
- `trades[].exitReason`：卖出原因说明。
- `trades[].breakout`：该笔交易依赖的突破识别详情，可直接用于图表回放。
- `trades[].buyPoint/sellPoint` 会在前端 K 线上标记为 `B/S`；鼠标悬停标记可查看详情，并显示对应价格虚线。
- `klines` / `windows`：本次回测实际使用的 K 线和滑动窗口结果，前端可直接复用，不需要重复调用上游。

当前内置方法 `breakout_band_follow` 规则：

- 在突破压力带的那根 K 线按突破阈值对应市值买入。
- 单个 token 同一时刻最多只保留 `1` 笔持仓；若前一笔尚未卖出，则后续买点信号全部跳过。
- 如果买入后下一根 K 线最低点跌破压力带上沿，则按压力带上沿止损卖出。
- 如果买入后任意后续 K 线最低点触发 `hardStopLossRate`，则按硬止损价卖出，默认 `0.05` 即 `-5%`。
- 如果后续最高点先达到 `activationProfitRate`，则从下一根 K 线开始启用动态止损；后续任意 K 线最低点跌到 `lockedProfitRate` 对应收益率时卖出。
- 如果最高点达到 `takeProfitRate`，则按止盈价卖出。
- 如果同时传 `takeProfitRateStart`、`takeProfitRateEnd`、`takeProfitRateStep`，则会在该范围内按步长逐个止盈比例执行回测，并按止盈比例分组返回盈亏情况。
- `feeRate` 表示单笔买入加卖出的总手续费比例，默认 `0.015`，统计时会从每笔收益里直接扣减。
- 如果样本结束前未触发止盈或止损，则按最后一根 K 线收盘价卖出。

说明：该接口内部按项目级 cache 复用 Birdeye K 线；某个 token + interval 首次缓存后，后续回测直接走 PostgreSQL，不再请求 Birdeye 最新 K 线。

### POST /api/market/birdeye/realtime-breakout-signals

基于“历史 K 线 + 当前实时 K 线”动态判断是否触发压力带突破信号。

这个接口的目标不是重新做整段回测，而是：

- 先用历史窗口识别出已经形成的试压结构
- 再用当前实时 K 线判断是否真正突破
- 一旦满足突破条件，就立即返回信号

请求体：

```json
{
  "tokenAddress": "token address",
  "interval": "1m",
  "startTime": "2026-06-25T08:00:00+08:00",
  "endTime": "2026-06-30T08:00:00+08:00",
  "levelOptions": {
    "windowSize": 120,
    "levelWindowSize": 120,
    "levelWindowStep": 20,
    "priceTolerance": 0.005,
    "minTouches": 3,
    "confirmBars": 1
  },
  "currentKline": {
    "interval": "1m",
    "openTime": "2026-06-30T08:00:00+08:00",
    "closeTime": "2026-06-30T08:01:00+08:00",
    "marketCapOpen": 101000,
    "marketCapHigh": 105000,
    "marketCapLow": 100500,
    "marketCapClose": 104200,
    "volume": 182000
  }
}
```

说明：

- `currentKline` 可选；如果传入，后端会把它当作最新实时 K 线参与信号判断。
- 若 `currentKline.openTime` 与历史最后一根相同，则用它覆盖最后一根。
- 若 `currentKline.openTime` 更晚，则把它追加为最新一根实时 K 线。
- 不传 `currentKline` 时，后端使用本次查到的最后一根 K 线做判断。
- 如果命中信号，后端除 HTTP 返回外，还会把同样的信号 JSON 发布到 Redis Pub/Sub channel，供独立消费程序订阅。

返回：

- `signals[]`：实时信号列表。
- `signals[].scenarioCode`：当前内置为 `pressure_breakout`。
- `signals[].windowIndex/levelIndex`：对应哪个场景窗口、哪个压力带。
- `signals[].signalTime/signalMarketCap`：信号触发时间与触发市值。
- `signals[].breakoutThreshold`：本次用于判断突破的实际阈值。
- `signals[].reason`：信号说明。
- `signals[].breakout`：用于回放的试压点、整理区和突破点详情。


### GET /api/market/db/support-resistance

根据数据库 K 线计算支撑位、压力位的通用接口。

参数：

- `pairId`：交易对 ID，对应 `bar_data.pair_id`。
- `interval`：K 线粒度，对应 `bar_data.interval`。
- `range=all`：可选，使用该 pair + interval 的全部 K 线。
- `startTime` / `endTime`：不传 `range=all` 时必填，北京时间 RFC3339。
- `pivotWindow`：可选，局部高低点窗口，默认 `5`。
- `priceTolerance`：可选，合并相近价位的基础容忍度，默认 `0.02`；实际计算会结合近 20 根 K 线 ATR 波动率自适应放大，最高 `0.08`。
- `breakTolerance`：可选，突破/跌破确认容忍度，默认 `0.01`。
- `confirmBars`：可选，连续确认 K 线数量，默认 `2`。
- `volumeWindow`：可选，成交量确认窗口，默认 `20`。
- `volumeMultiplier`：可选，放量突破倍数，默认 `1.2`。
- `maxLevels`：可选，每类最多返回多少个强支撑/压力位，默认 `8`。

返回：

- `levels`：支撑/压力位数组。
- `type`：`support` 或 `resistance`。
- `price/lower/upper`：中位价格和价格带。
- `touches`：触碰次数。
- `score`：强度分。
- `status`：`holding`、`rejected`、`support_broken`、`resistance_broken` 等。

计算规则：

- 支持任意时间粒度 K 线；核心算法只依赖传入的 OHLCV 序列，不绑定具体 `interval`。
- 使用局部高低点识别候选价位：局部低点生成支撑候选，局部高点生成压力候选。
- 按价格容忍度把相近候选合并成价格带，并统计所有 K 线对该价格带的触碰次数和成交量，而不是只统计 pivot 点。
- 根据最新收盘价重新判定角色：位于当前价下方的价格带视为支撑，位于当前价上方的价格带视为压力，用于支持“压力突破后转支撑、支撑跌破后转压力”。
- 强度分综合触碰次数、价格带成交量、最近一次触碰时间、距离当前价远近；每类按强度返回前 `maxLevels` 条。

## 创建回测分析

### POST /api/backtests

请求体：

```json
{
  "dataSource": "birdeye",
  "tokenAddress": "token address",
  "tokenSymbol": "TOKEN",
  "interval": "1m",
  "startTime": "2026-06-22T08:00:00+08:00",
  "endTime": "2026-06-22T09:00:00+08:00",
  "tradePoints": [
    { "side": "buy", "time": "2026-06-22T08:10:00+08:00", "note": "买入" },
    { "side": "sell", "time": "2026-06-22T08:20:00+08:00", "note": "卖出" }
  ]
}
```

返回：

- `sessionId`：分析会话 ID。
- `klines`：本次分析使用的 K 线。
- `tradePoints`：匹配 K 线后的买卖点。
- `trades`：逐笔交易结果。
- `metrics`：累计收益、胜率、最大回撤、交易次数、平均持仓时间。

规则：

- 买卖点必须按买入、卖出顺序成对出现。
- 买卖点价格按对应或最近后续 K 线的 `close` 价计算。
- 第一版不计算滑点、手续费、成交失败或流动性约束。
- 创建成功后会写入业务库；没有可写数据库时接口直接失败。

## 查询回测分析

### GET /api/backtests/:id

返回已保存的分析会话、买卖点、逐笔结果和指标快照。

## 回测分析列表

### GET /api/backtests

返回最近 50 条分析会话。


## 交易模块接口

交易模块当前与回测、信号模块解耦：

- `signal` 负责识别实时突破结构并向 Redis 发布统一交易信号
- `trade` 负责消费信号、记录订单/成交/持仓，并通过 DexScreener 刷新持仓估值
- 当前交易模块已支持真实 Jupiter 执行；买入默认用 SOL 作为输入资产，并把 `trade.buy_amount_usd` 先折算成 SOL 数量后再向 Jupiter 下单。
- DexScreener 与 Jupiter 的外网请求固定通过服务器本机 clash 代理 `http://127.0.0.1:7890`。

### GET /api/trade/accounts

返回交易账户列表。当前默认设计为单钱包单账户。

### GET /api/trade/signals

参数：

- `limit`：可选，默认 `100`。

返回交易模块已接收的标准化信号，字段包含：

- `signalId`
- `signalType`：`buy` / `sell`
- `strategyCode`
- `tokenAddress`
- `interval`
- `signalTime`
- `triggerPrice`
- `triggerMarketCap`
- `reason`
- `consumeStatus`

### GET /api/trade/orders

参数：

- `limit`：可选，默认 `100`。

返回订单列表，字段包含：

- `side`：买/卖方向
- `intentAmountUsd` / `intentTokenAmount`：下单意图金额
- `status`：`pending` / `submitted` / `filled` / `failed`
- `submitTxHash`
- `failReason`

### GET /api/trade/orders/:id

返回单笔订单详情。

### POST /api/trade/orders/:id/retry

按原订单对应的信号重新触发一次执行。

### GET /api/trade/positions

参数：

- `status`：可选，`open` / `closed`
- `limit`：可选，默认 `100`

返回持仓列表，字段包含：

- `quantity`
- `costAmount`
- `avgCostPrice`
- `lastPrice`
- `marketValue`
- `realizedPnl`
- `unrealizedPnl`
- `maxProfitRate`
- `maxDrawdownAmount`

### POST /api/trade/positions/:id/close

手动发起某个持仓的卖出流程。
