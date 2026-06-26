# Solana Meme Backtest V2

基于 Birdeye 市值 K 线的 Solana meme 币支撑/压力位分析、回测与交易信号系统。

当前版本重点覆盖三块能力：

- `signal`：根据滑动窗口 K 线识别压力/支撑结构，并输出实时突破信号
- `backtest`：基于结构突破场景做历史回测、收益统计与买卖点回放
- `trade`：消费 Redis 信号，记录订单/成交/持仓，并维护持仓估值

## 技术栈

- 后端：Go、Gin、Viper、Zerolog、PostgreSQL
- 前端：Vue 3、Vite、Pinia、Vue Router、Element Plus、Vitest
- 数据源：Birdeye、DexScreener、Redis

## 目录结构

```text
.
├── backend/
│   ├── cmd/server/
│   ├── config/
│   └── internal/
│       ├── api/          # HTTP API
│       ├── backtest/     # 支撑/压力位与回测核心
│       ├── datasource/   # Birdeye / SQL / DexScreener 数据源
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

### 1. Birdeye K 线缓存

- 同一个 `tokenAddress + interval` 首次调用 Birdeye 成功后写入 PostgreSQL cache
- 后续优先复用项目缓存，不重复请求最新 K 线
- 支持 Birdeye API key 池，遇到 `429` 或额度耗尽自动轮换

### 2. 支撑带 / 压力带计算

- 使用滑动窗口和局部高低点聚类生成价格带
- 当前前端统一按近 5 天范围加载
- 压力带上下边界基于 `priceTolerance` 与 ATR 自适应放宽
- 强度分综合触碰次数、成交量、最近性、与当前市值距离

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

- `POST /api/market/birdeye/realtime-breakout-signals` 会同步返回命中的突破信号
- 若配置了 Redis，信号模块会把命中的结构突破转换成标准化交易消息后发布到 channel
- `trade` 模块负责：
  - 消费 Redis 信号
  - 保证同一账户同一 token 同时最多一笔 open position
  - 写入 `trade_signals / trade_orders / trade_fills / trade_positions / trade_order_events`
  - 通过 DexScreener 刷新 open position 的最新估值

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

说明：当前交易模块已支持真实 Jupiter 下单链路：`下单 -> 本地签名 -> 执行`。买入默认使用 SOL 作为输入资产，并按实时 SOL/USD 价格把 `trade.buy_amount_usd` 折算成 SOL 数量后下单。

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
- `birdeye.api_key` 或 `birdeye.api_keys`
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
- `birdeye.api_key` / `birdeye.api_keys`：Birdeye key 与 key 池
- `redis.enabled` / `redis.addr` / `redis.channel`：实时信号发布与交易消费通道
- `trade.enabled`：是否启用交易模块
- `trade.signal_consumer`：是否订阅 Redis 信号并自动执行
- `trade.price_sync_enabled`：是否定时刷新 open positions 估值
- `trade.buy_amount_usd`：固定买入金额
- `trade.wallet_private_key`：Solana 钱包私钥（base58）
- `trade.solana_rpc_url`：用于查询 token decimals 的 Solana RPC
- `trade.dexscreener.base_url`：持仓估值接口
- `trade.jupiter.base_url`：Jupiter Ultra API 入口
- `trade.jupiter.api_key`：Jupiter API Key

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

- `GET /api/market/birdeye/klines`
- `GET /api/market/birdeye/support-resistance`
- `POST /api/market/birdeye/realtime-breakout-signals`
- `POST /api/strategy-backtests/run`
- `GET /api/trade/accounts`
- `GET /api/trade/orders`
- `GET /api/trade/positions`

## 开发约束

开发前先看：`docs/项目开发规范.md`
