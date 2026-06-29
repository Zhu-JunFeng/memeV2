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

前端约定：

- `frontend/src/api/http.js` 统一解包该结构；当后端返回 `code != 0` 或 HTTP 请求失败时，前端会用 Element Plus 通知组件在右上角提示错误信息，并附带 `traceId`（如有），同时继续向调用方抛出错误供页面状态展示。SSE 连接异常也会通过同一通知样式提示。

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
- `source`：可选，`gmgn` / `birdeye` / `sql` / `db`；不传时使用 `datasource.kline_source` 配置，当前默认 `gmgn`。

返回：

- `items`：K 线数组，字段包含 `openTime`、`closeTime`、`open`、`high`、`low`、`close`、`marketCapOpen`、`marketCapHigh`、`marketCapLow`、`marketCapClose`、`volume`。GMGN 返回的是 USD 价格，后端会把同一组价格同时写入 `open/high/low/close` 与算法沿用的 `marketCap*` 字段。

配置：

- `BACKTEST_DATASOURCE_KLINE_SOURCE` / `datasource.kline_source`：默认 K 线数据源，支持 `gmgn` / `birdeye` / `sql` / `db`，当前默认 `gmgn`。
- `BACKTEST_GMGN_API_KEY` / `gmgn.api_key`：GMGN API Key；选择 GMGN 时必填。
- `BACKTEST_GMGN_BASE_URL` / `gmgn.base_url`：默认 `https://openapi.gmgn.ai`。
- `BACKTEST_GMGN_CHAIN` / `gmgn.chain`：默认 `sol`。
- `BACKTEST_GMGN_MAX_QPS` / `gmgn.max_qps`：进程内 GMGN 请求限速，默认 `8`，低于实测约 `10 QPS` 的硬上限。

说明：SQL 数据源依赖 `BACKTEST_DATASOURCE_KLINE_QUERY` 或配置文件 `datasource.kline_query`。SQL 参数顺序固定为 `tokenAddress`、`interval`、`startTime`、`endTime`。

### GET /api/market/gmgn/klines

GMGN K 线专用入口。参数同 `/api/market/klines`，但固定使用 GMGN 数据源。

说明：GMGN `token_kline` 接口的 `from/to` 使用毫秒时间戳，后端会自动转换；`volume` 为 USD 成交额，`amount` 不进入当前统一 K 线模型。活跃项目的当前 1m K 线会在同一个 `openTime` 下持续更新，最新一根 `close` 可作为近实时价格。

### GET /api/market/birdeye/klines

Birdeye K 线专用入口。参数同 `/api/market/klines`，但固定使用 Birdeye 数据源。

配置：

- `BACKTEST_BIRDEYE_API_KEY`：Birdeye API Key，必填。
- `BACKTEST_BIRDEYE_API_KEYS`：可选，Birdeye API Key 池，支持逗号分隔多个 key；当某个 key 遇到 `429` 或额度耗尽时，后端会自动切换到下一个 key。
- `BACKTEST_BIRDEYE_BASE_URL`：默认 `https://public-api.birdeye.so`。
- `BACKTEST_BIRDEYE_CHAIN`：默认 `solana`。
- `BACKTEST_DATABASE_DSN`：PostgreSQL 连接串；Birdeye K 线缓存、回测结果、交易模块表统一存储在同一个 PG 库。

说明：未配置 API Key 时直接返回中文错误，不改用 SQL 或其他数据源。Birdeye K 线首次拉取成功后会写入 PostgreSQL cache；同一个 token + interval 只要本地已经缓存过，后续都直接优先读取该项目缓存，不再为了追最新 K 线重复请求 Birdeye。Birdeye 原始 `volume` 为 token 成交数量，系统在回测/结构识别里会按 `token volume * close price` 转成成交额口径，与 GMGN 的 `volume` 语义对齐。


### GET /api/market/support-resistance

根据 CA 获取指定数据源的 K 线，并基于这批 K 线计算支撑位、压力位。`source` 不传时使用 `datasource.kline_source`，当前默认 GMGN。

### GET /api/market/gmgn/support-resistance

GMGN K 线专用支撑/压力位入口。参数同 `/api/market/support-resistance`，但固定使用 GMGN。

### GET /api/market/birdeye/support-resistance

Birdeye K 线专用支撑/压力位入口。参数同 `/api/market/support-resistance`，但固定使用 Birdeye。

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

- `klines`：K 线数组，字段包含 `open`、`high`、`low`、`close`、`marketCapOpen`、`marketCapHigh`、`marketCapLow`、`marketCapClose`。GMGN 源下这些数值为 USD 价格；Birdeye 源下仍为原来的市值口径。
- `windowSize`：实际生效的窗口 K 线数量。
- `windowStep`：窗口滑动步长，当前固定为 `1`。
- `windows`：滑动窗口结果数组。
- `windows[].startTime/endTime`：该窗口覆盖的 K 线时间范围。
- `windows[].klineCount`：该窗口包含的 K 线数量。
- `windows[].levels`：该窗口下的支撑/压力位数组，字段沿用 `marketCap`、`lowerMarketCap`、`upperMarketCap` 命名；GMGN 源下实际含义为 USD 价格带。
- `windows[].levels[].calculation`：该市值位的计算依据，包含 pivot 数量、支撑/压力票数、价格容忍度、当前市值、类型判定原因、状态判定原因、强度分拆解、形成市值位的局部高低点和触碰样本。
- `windows[].levels[].breakout`：压力位突破回测结果；会返回试压点 `failedTouches`、突破点 `breakoutPoint`、买入点 `buyPoint`、止损止盈价格、退出点、收益率，以及最终是先止损、先止盈还是超时平仓。当前场景还要求：试压组与突破点之间，最多只允许 `1` 根 K 线的最高价刺穿压力带上沿，超过则不算有效场景。
- 在试压组内部，从第一根试压阳线到最后一根试压阳线之间，若收盘价高于压力带上沿的 K 线超过 `3` 根，该场景直接过滤，不进入回测。

说明：选择哪个 `source` 就只调用对应数据源，不自动改用其他数据源。GMGN 未配置 API Key 时直接返回中文错误。Birdeye 专用入口仍保留原项目级 PostgreSQL cache 语义。

### GET /api/strategy-backtests/methods

返回可用回测方法列表，供前端按方法编码和参数定义渲染配置表单。

返回：

- `items[].code`：方法编码。
- `items[].name`：方法名称。
- `items[].description`：方法说明。
- `items[].params`：参数定义，当前包含 `key`、`label`、`description`、`type`、`defaultValue`、`minValue`、`maxValue`、`step`。

### POST /api/strategy-backtests/run

按指定 CA、区间和回测方法执行策略回测。默认使用 `datasource.kline_source`，当前为 GMGN；请求体可传 `dataSource` 切换到 `birdeye` / `sql` / `db`。

请求体：

```json
{
  "dataSource": "gmgn",
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

说明：该接口不再写死 Birdeye。`dataSource=gmgn` 时直接按请求区间调用 GMGN；`dataSource=birdeye` 时沿用 Birdeye 项目级 cache。

### POST /api/market/realtime-breakout-signals

基于“历史 K 线 + 当前实时 K 线”动态判断是否触发压力带突破信号。默认使用 `datasource.kline_source`，当前为 GMGN；请求体可传 `dataSource` 切换。

### POST /api/market/gmgn/realtime-breakout-signals

GMGN K 线专用实时突破信号入口。

### POST /api/market/birdeye/realtime-breakout-signals

Birdeye K 线专用实时突破信号入口。

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

### POST /api/birdeye/api-keys

向数据库中的 Birdeye API Key 池新增一个 key。新增后的 key 默认状态为 `available`，后续所有直接调用 Birdeye 的路径会从数据库中轮流选择可用 key。

请求体：

```json
{
  "apiKey": "birdeye api key"
}
```

返回：

- `id`：Key 记录 ID。
- `keyMask`：脱敏后的 key 展示值。
- `status`：当前状态，新增后为 `available`。
- `unavailableReason`：不可用原因，新增后为空。
- `unavailableAt`：被标记不可用的时间，新增后为空。

说明：

- 接口不会返回 API Key 原文。
- 如果新增的 key 已存在，会把该 key 重新标记为 `available`，清空不可用原因。
- 当 Birdeye 返回 `Compute units usage limit exceeded` 时，V2 会把当前使用的 key 标记为 `unavailable`，并继续用池内其他可用 key 补偿请求，直到数据库中没有可用 key。


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
- `trade` 负责消费信号、记录订单/成交/持仓，并通过 `trade.price_source` 刷新持仓估值；当前默认 GMGN，可切回 DexScreener
- 如配置 `redis.consumer_channel`，交易消费订阅该通道；未配置时沿用 `redis.channel`
- 交易消费只处理标准 `TradeSignalMessage`；老版候选池 `candidate_score_passed` 由信号模块订阅后进入二次监控，不再直接买入
- 候选池二次监控每 2 秒调用 GMGN 实时价格接口，按最新 USD 价格乘 Solana RPC `getTokenSupply` 聚合本地 1m 市值 K 线；重启时会先从 `system_kline_cache` 预加载近 200 根，再继续增量维护，出现 `breakout_band_follow` 买点/卖点后发布标准交易信号
- 未买入候选在最新市值低于阈值时从监控池移除；默认阈值为 `10_000`，配置了正数 `signal.min_market_cap` 时按配置值覆盖，已买入候选继续监控卖点
- 候选池监控只用“最后一根已收盘 bar”做突破与卖点判定；未收盘 bar 只更新当前市值，不直接触发买卖信号
- 候选池自维护 K 线的 `volume` 表示该分钟累计采样次数，用这组系统内量能执行试压/突破量能过滤
- 候选池卖出后如果市值仍高于阈值会重新进入 `watching`，但同一根已卖出的 bar 不允许再次买入，实时语义与回测的单持仓约束保持一致
- 交易模块支持全局 `paper/live` 两种模式，模式值持久化在数据库 `system_runtime_settings`
- `paper` 模式只调用 Jupiter `quote` 报价接口，不依赖真实钱包余额，也不会签名和执行；系统会基于报价结果生成模拟成交
- `live` 模式保持真实 Jupiter 执行；买入默认用 SOL 作为输入资产，并把 `trade.buy_amount_usd` 先折算成 SOL 数量后再向 Jupiter 下单
- GMGN、Jupiter 的外网请求固定通过服务器本机 clash 代理 `http://127.0.0.1:7890`；DexScreener 仅在 `trade.price_source=dexscreener` 时使用。

### GET /api/signal/candidate-monitor

返回当前仍在 V2 信号模块 active 监控池里的上游候选项目，供前端展示“上游发出来但尚未一定触发买卖”的项目。

返回字段：

- `items[].tokenAddress`：候选项目 CA。
- `items[].symbol`：上游发来的 symbol。
- `items[].status`：当前监控状态，`watching` 表示等待压力带突破，`bought` 表示已发出买入信号并继续等待卖点。
- `items[].candidateAt`：候选项目进入 V2 监控池的时间。
- `items[].strategyName` / `items[].scanNo`：上游评分策略名和扫描批次。
- `items[].upstreamScore` / `items[].upstreamMarketCap`：上游评分合格信号内携带的评分和市值。
- `items[].currentMarketCap` / `items[].currentMarketCapAt`：V2 最近一次成功计算出的当前市值和对应 K 线时间；GMGN 源下按最新 USD 价格乘 Solana RPC `getTokenSupply` 当前总供应量计算。
- `items[].birdeyeKlineFetchedAt`：兼容字段名，表示 V2 最近一次成功拉取实时价格并更新本地 1m 市值 K 线的时间，前端按相对时间展示为 `2s前`、`4s前` 等。
- `items[].buySignalId`：如果已触发 V2 买入信号，这里返回对应信号 ID。
- `items[].entryTime` / `items[].entryMarketCap`：如果已触发买入信号，这里返回买点时间和买点市值。
- `items[].levelMarketCap` / `items[].levelLowerMarketCap` / `items[].levelUpperMarketCap`：如果已触发买入信号，这里返回当时突破的压力带。

说明：该接口只读取 Redis active 监控池，不查询历史已停止、已卖出或已被低市值过滤移除的候选项目。

### POST /api/signal/candidate-monitor

手动把一个 CA 加入 V2 active 监控池。请求只需要传 `tokenAddress`，后端会按 `watching` 状态写入 Redis，并通过 Candidates SSE 推送 `upsert`。

请求体：

```json
{
  "tokenAddress": "CA"
}
```

返回：

- `item`：新增或已存在的候选池项目。

### GET /api/signal/candidate-monitor/stream

Candidates 实时 SSE 流。连接后先发送 `event: snapshot`，数据为 `{ "items": [...] }`；之后候选池状态变化发送 `event: upsert`，数据为 `{ "item": {...} }`；候选移出 active 池发送 `event: delete`，数据为 `{ "id": "<tokenAddress>" }`；每 15 秒发送 `event: heartbeat`。

### GET /api/trade/accounts

返回交易账户列表。当前默认设计为单钱包单账户。

### GET /api/trade/runtime

返回当前全局交易模式。

返回：

- `tradeMode`：当前模式，`paper` 或 `live`
- `options[]`：前端可直接渲染的模式选项

### PUT /api/trade/runtime

切换并持久化全局交易模式。

请求体：

```json
{
  "tradeMode": "paper"
}
```

说明：

- `paper`：只请求 Jupiter `quote`，不依赖真实钱包余额，也不会执行签名和链上提交
- `live`：恢复真实下单执行
- 切换后新进来的信号、订单、成交、持仓都会记录对应 `tradeMode`

### GET /api/trade/summary

返回交易汇总，固定包含三组：

- `items[].tradeMode`：`all` / `paper` / `live`
- `items[].totalPnl`：总盈亏，口径为 `realizedPnl + unrealizedPnl`
- `items[].realizedPnl`：已实现盈亏
- `items[].unrealizedPnl`：未实现盈亏，仅统计 open 持仓
- `items[].tradeCount`：已平仓笔数
- `items[].winCount` / `items[].lossCount`
- `items[].winRate`：`winCount / tradeCount`
- `items[].openPositionCount` / `items[].closedPositionCount`
- `items[].maxDrawdownAmount`：该模式下最大回撤金额
- `items[].updatedAt`：该模式下最近一次持仓更新时间

说明：

- 汇总口径基于 `trade_positions`
- 没有数据时各字段返回 `0`，`updatedAt` 为空

### GET /api/trade/signals

参数：

- `limit`：可选，默认 `100`。
- `tradeMode`：可选，`all` / `paper` / `live`，默认 `all`。

返回交易模块已接收的标准化信号，字段包含：

- `signalId`
- `tradeMode`
- `signalType`：`buy` / `sell`
- `strategyCode`
- `tokenAddress`
- `interval`
- `signalTime`
- `triggerPrice`
- `triggerMarketCap`
- `reason`
- `consumeStatus`

### GET /api/trade/signals/stream

Signals 实时 SSE 流。参数同 `/api/trade/signals`。连接后先推 `snapshot`，之后新信号或消费状态变化推 `upsert`，每 15 秒推 `heartbeat`。

### GET /api/trade/orders

参数：

- `limit`：可选，默认 `100`。
- `tradeMode`：可选，`all` / `paper` / `live`，默认 `all`。

返回订单列表，字段包含：

- `tradeMode`
- `executionChannel`：当前为 `jupiter_paper` 或 `jupiter_live`
- `side`：买/卖方向
- `intentAmountUsd` / `intentTokenAmount`：下单意图金额
- `status`：`pending` / `submitted` / `filled` / `failed`
- `submitTxHash`
- `failReason`

### GET /api/trade/orders/stream

Orders 实时 SSE 流。参数同 `/api/trade/orders`。连接后先推 `snapshot`，之后订单创建、提交、失败、成交等状态变化推 `upsert`，每 15 秒推 `heartbeat`。

### GET /api/trade/orders/:id

返回单笔订单详情。

### POST /api/trade/orders/:id/retry

按原订单对应的信号重新触发一次执行。

### GET /api/trade/positions

参数：

- `status`：可选，`open` / `closed`
- `limit`：可选，默认 `100`
- `tradeMode`：可选，`all` / `paper` / `live`，默认 `all`

返回持仓列表，字段包含：

- `tradeMode`
- `quantity`
- `costAmount`
- `avgCostPrice`
- `lastPrice`
- `marketValue`
- `realizedPnl`
- `unrealizedPnl`
- `maxProfitRate`
- `maxDrawdownAmount`

### GET /api/trade/positions/stream

Positions 实时 SSE 流。参数同 `/api/trade/positions`。连接后先推 `snapshot`，之后持仓创建、估值刷新、平仓等变化推 `upsert`；如果订阅了 `status=open` 而持仓已平仓，会推 `delete`。每 15 秒推 `heartbeat`。

### POST /api/trade/positions/:id/close

手动发起某个持仓的卖出流程。
