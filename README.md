# Solana Meme Backtest V2

基于可切换 K 线数据源的 Solana meme 币支撑/压力位分析、回测与交易信号系统；当前默认使用 GMGN，保留 Birdeye / SQL / DB 切换能力。

当前版本重点覆盖三块能力：

- `signal`：根据滑动窗口 K 线识别压力/支撑结构，并输出实时突破信号
- `backtest`：基于结构突破场景做历史回测、收益统计与买卖点回放
- `trade`：消费 Redis 信号，记录订单/成交/持仓，并维护持仓估值

## 技术栈

- 后端：Go、Gin、Viper、Zerolog、PostgreSQL
- 前端：Vue 3、Vite、Pinia、Vue Router、Element Plus、Vitest
- 数据源：GMGN、Birdeye、DexScreener、Redis

## 目录结构

```text
.
├── backend/
│   ├── cmd/server/
│   ├── config/
│   └── internal/
│       ├── api/          # HTTP API
│       ├── backtest/     # 支撑/压力位与回测核心
│       ├── datasource/   # GMGN / Birdeye / SQL / DexScreener 数据源
│       ├── db/           # PostgreSQL 初始化与表迁移
│       ├── repository/   # 回测/交易持久化
│       ├── signal/       # 结构识别 + Redis 信号发布
│       ├── trade/        # 交易执行、持仓、价格同步
│       └── response/
├── frontend/
├── docs/
└── .deploy/
```

## 核心能力

### 1. K 线数据源

- 默认 K 线源为 GMGN：`datasource.kline_source=gmgn`，覆盖回测、实时信号、候选池二次监控等 K 线读取入口
- 前端“加载 K 线并计算”按钮右侧提供小型数据源选择框，可直接在 `gmgn` / `birdeye` 之间切换；接口仍可通过 `source` / `dataSource` 指定数据源
- GMGN 返回 USD 价格 K 线；为复用现有算法，后端同时填充 `open/high/low/close` 与算法沿用的 `marketCap*` 字段
- Birdeye 原数据源继续保留，首次调用成功后仍写入 PostgreSQL cache，并支持 API key 池轮换

### 2. 支撑带 / 压力带计算

- 使用滑动窗口和局部高低点聚类生成价格带
- 当前前端统一按近 5 天范围加载
- 压力带上下边界基于 `priceTolerance` 与 ATR 自适应放宽
- 强度分综合触碰次数、成交量、最近性、与当前市值距离
- K 线图内置常用指标与基础画线工具，当前支持 `VOL / MA7 / MA20 / MA60 / EMA20 / BOLL` 以及水平线、趋势线，作为 GMGN 风格图表工作台的第一版能力

### 3. 试压与突破场景

当前试压规则：

- 必须是阳线
- 最高点必须进入压力带
- 试压阳线成交量必须达到更高门槛
- 试压组与突破点之间，最多只允许 `1` 根 K 线的最高点刺穿压力带上沿
- 从第一根试压到最后一根试压之间，收盘价高于压力带上沿的 K 线超过 `3` 根则过滤

### 4. 回测规则

当前内置方法：`breakout_band_follow`

- 买入点：突破压力带的那根 K 线在突破阈值对应的市值
- 单个 token 同时最多保留 `1` 笔持仓
- 下一根 K 线跌破压力带上沿：止损
- 支持硬止损、动态锁盈、止盈区间扫描、固定手续费
- 买卖点会在前端 K 线上标记 `B/S`

### 5. 实时信号与交易模块

- `POST /api/market/realtime-breakout-signals` 会按当前 K 线数据源同步返回命中的突破信号；GMGN/Birdeye 专用入口也保留
- 若配置了 Redis，信号模块会把命中的结构突破转换成标准化交易消息后发布到 channel
- `signal` 模块可订阅上游候选池评分合格事件，进入 Redis 监控池后每 2 秒查询当前 K 线源的最新 1m K 线，出现 `breakout_band_follow` 买点/卖点后发布标准交易信号
- `trade` 模块负责：
  - 消费标准 Redis 交易信号；如配置 `redis.consumer_channel`，交易消费使用该独立通道
  - 保证同一账户同一 token 同时最多一笔 open position
  - 写入 `trade_signals / trade_orders / trade_fills / trade_positions / trade_order_events`
  - 通过 `trade.price_source` 刷新 open position 的最新估值，当前默认 GMGN，可切换 DexScreener

当前 Redis 信号消息结构：

```json
{
  "signalId": "uuid",
  "signalType": "buy",
  "strategyCode": "pressure_breakout",
  "tokenAddress": "token address",
  "interval": "1m",
  "signalTime": "2026-06-26T10:00:00+08:00",
  "triggerPrice": 123456,
  "triggerMarketCap": 123456,
  "reason": "压力带实时突破: ...",
  "metadata": {"windowIndex": 1, "levelIndex": 2}
}
```

说明：

- 当前交易模块支持全局 `模拟盘 / 实盘` 两种模式，模式值落库到 `system_runtime_settings`，服务重启后继续生效。
- 模拟盘只调用 Jupiter `quote` 报价接口，不依赖真实钱包余额，也不会签名和执行链上交易；系统会基于报价结果模拟 fill，并把相关订单、成交、持仓都打上 `paper` 标记。
- 实盘保持原链路：`下单 -> 本地签名 -> 执行`。买入默认使用 SOL 作为输入资产，并按实时 SOL/USD 价格把 `trade.buy_amount_usd` 折算成 SOL 数量后下单。
- GMGN、DexScreener 与 Jupiter 的外网请求当前固定走服务器本机 clash 代理 `http://127.0.0.1:7890`。

## 本地开发

### 环境要求

- Go 1.23+
- Node.js 18+
- PostgreSQL 14+
- Redis（仅在需要实时信号发布/交易消费时启用）

### 后端启动

```bash
cd backend
cp config/config.example.yaml config.yaml
go run ./cmd/server
```

至少配置这些项：

- `database.dsn`
- `gmgn.api_key`（默认 GMGN K 线源必填）
- `birdeye.api_key` 或 `birdeye.api_keys`（切换到 Birdeye 或拉交易点时使用）
- 若启用实时交易：`redis.*`、`trade.*`

健康检查：

```bash
curl http://127.0.0.1:8890/api/health
```

### 前端启动

```bash
cd frontend
npm install
npm run dev
```

## 测试与构建

后端测试：

```bash
cd backend
mkdir -p .tmp/go-build-cache
GOCACHE=$PWD/.tmp/go-build-cache go test ./...
```

后端 Linux 构建：

```bash
cd backend
mkdir -p .tmp/go-build-cache
GOCACHE=$PWD/.tmp/go-build-cache GOOS=linux GOARCH=amd64 go build -o solana-meme-backtest ./cmd/server
```

前端测试与构建：

```bash
cd frontend
npm test
npm run build
```

## 配置说明

示例配置见 `backend/config/config.example.yaml`。

常用配置项：

- `database.dsn`：PostgreSQL 连接串
- `database.auto_migrate`：启动时自动建表
- `datasource.kline_source`：默认 K 线源，支持 `gmgn` / `birdeye` / `sql` / `db`，当前默认 `gmgn`
- `gmgn.api_key` / `gmgn.max_qps`：GMGN key 与进程内限速；候选池监控当前按轮询实时价格聚合本地 1m 市值 K 线，默认按 8 QPS 留余量
- `birdeye.api_key` / `birdeye.api_keys`：Birdeye key 与 key 池
- `redis.enabled` / `redis.addr` / `redis.channel`：实时信号发布通道，未配置消费通道时也作为交易消费通道
- `redis.consumer_channel`：交易模块独立订阅通道；为空时消费 `redis.channel`
- `signal.candidate_monitor_enabled` / `signal.candidate_channel`：是否启用候选池后二次压力突破监控及上游候选池通道
- `signal.poll_interval_seconds` / `signal.min_market_cap`：候选池监控轮询间隔与未买入低市值移除阈值；候选池当前默认阈值为 `10_000`，配置 `>0` 时按配置值覆盖
- 候选池监控只对“最后一根已收盘 1m 市值 K 线”做买卖判定，避免把未收盘 bar 当成突破/止损输入
- 候选池自维护 K 线的 `volume` 表示该分钟内的系统采样次数，用来执行实时量能过滤，不再固定为 `0`
- 候选池卖出后若市值仍高于阈值会 rearm，但同一根已卖出的 bar 不允许立刻重新买入
- `trade.enabled`：是否启用交易模块
- `trade.signal_consumer`：是否订阅 Redis 信号并自动执行
- `trade.price_sync_enabled`：是否定时刷新 open positions 估值
- `trade.buy_amount_usd`：固定买入金额
- `trade.wallet_private_key`：Solana 钱包私钥（base58）
- `trade.solana_rpc_url`：用于查询 token decimals 的 Solana RPC
- `trade.price_source`：持仓估值和 SOL/USD 折算价格源，支持 `gmgn` / `dexscreener`，当前默认 `gmgn`
- `trade.dexscreener.base_url`：DexScreener 持仓估值接口，仅在 `trade.price_source=dexscreener` 时使用
- `trade.jupiter.base_url`：Jupiter Ultra API 入口
- `trade.jupiter.api_key`：Jupiter API Key
- GMGN、Jupiter HTTP 客户端固定通过服务器本机 clash 代理 `http://127.0.0.1:7890` 出网；DexScreener 仅在启用对应价格源时使用固定代理
- 交易模式不通过配置文件固定，而是通过页面或 `/api/trade/runtime` 动态切换并持久化到数据库

## 部署

当前线上部署目标：

- URL：`http://182.92.160.46/`
- 后端目录：`/data/solana-scalper-v2/backend`
- 前端目录：`/data/solana-scalper-v2/frontend`
- systemd 服务：`solana-meme-backtest-v2`

当前部署方式：

- 后端：本地交叉编译 Linux 二进制后上传并重启服务
- 前端：构建 `frontend/dist` 后同步到服务器静态目录

## 主要接口

详见 `docs/API.md`。

常用接口：

- `GET /api/market/klines`
- `GET /api/market/gmgn/klines`
- `GET /api/market/support-resistance`
- `GET /api/market/gmgn/support-resistance`
- `POST /api/market/realtime-breakout-signals`
- `POST /api/market/gmgn/realtime-breakout-signals`
- `GET /api/market/birdeye/klines` / `GET /api/market/birdeye/support-resistance`（保留切换）
- `POST /api/strategy-backtests/run`
- `GET /api/signal/candidate-monitor` / `GET /api/signal/candidate-monitor/stream`
- `POST /api/signal/candidate-monitor`：手动输入 CA 加入 Candidates active 监控池
- `GET /api/trade/accounts`
- `GET /api/trade/runtime`
- `PUT /api/trade/runtime`
- `GET /api/trade/summary`
- `GET /api/trade/signals` / `GET /api/trade/signals/stream`
- `GET /api/trade/orders` / `GET /api/trade/orders/stream`
- `GET /api/trade/positions` / `GET /api/trade/positions/stream`

## 开发约束

开发前先看：`docs/项目开发规范.md`
