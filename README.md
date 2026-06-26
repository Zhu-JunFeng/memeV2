# Solana Meme Backtest V2

基于 Birdeye 市值 K 线的 Solana meme 币支撑/压力位分析与回测系统。

当前版本重点解决这几类问题：

- 根据 `CA` 拉取并缓存 Birdeye 市值 K 线
- 用滑动窗口计算支撑带、压力带及其形成依据
- 识别“多次试压 -> 有效突破”的压力突破场景
- 基于突破买点执行单持仓回测，并展示买卖点、收益、回撤和卖出原因
- 在前端图表中回放压力带、试压点、突破点、`B/S` 买卖标记和收益明细

## 技术栈

- 后端：Go、Gin、GORM、Viper、Zerolog、SQLite
- 前端：Vue 3、Vite、Pinia、Vue Router、Element Plus、Vitest
- 数据源：Birdeye

## 目录结构

```text
.
├── backend/                  # Go 后端
│   ├── cmd/server/           # 服务启动入口
│   ├── config/               # 配置示例
│   └── internal/
│       ├── api/              # HTTP 路由
│       ├── backtest/         # 支撑/压力位、突破场景、回测核心逻辑
│       ├── datasource/       # Birdeye / SQL / 交易点数据源实现
│       ├── db/               # SQLite 初始化
│       ├── model/            # 领域模型
│       ├── repository/       # 业务库持久化
│       └── response/         # 统一响应结构
├── frontend/                 # Vue 前端
│   ├── src/api/              # API 请求封装
│   ├── src/components/       # 图表和回测组件
│   ├── src/stores/           # Pinia 状态
│   └── src/views/            # 页面
├── docs/
│   ├── API.md                # 接口文档
│   ├── 项目开发规范.md        # 仓库开发约束
│   └── 需求变更/              # 需求演进记录
└── .deploy/                  # 当前项目保留的部署产物/脚本
```

## 核心能力

### 1. Birdeye K 线缓存

- 同一个 `tokenAddress + interval` 首次调用 Birdeye 成功后写入 SQLite cache
- 后续优先复用项目缓存，不重复请求最新 K 线
- 支持 Birdeye API key 池，遇到 `429` 或额度耗尽自动切换

### 2. 支撑带 / 压力带计算

- 使用滑动窗口和局部高低点聚类生成价格带
- 当前前端统一按“近 5 天”范围加载
- 压力带上下边界基于 `priceTolerance` 计算，并结合 ATR 自适应放宽
- 强度分综合：
  - 触碰次数
  - 成交量
  - 最近性
  - 与当前市值的距离

### 3. 试压与突破场景

当前试压规则：

- 必须是阳线
- 最高点必须进入压力带
- 试压阳线成交量必须达到更高门槛：
  - 最近 `volumeWindow` 根历史均量的至少 `1.35x`
  - 与 `volumeMultiplier` 取较高值
- `n` 根试压阳线之后，后续 `n` 根 K 线内必须出现有效突破
- 试压组与突破点之间，最多只允许 `1` 根 K 线的最高点刺穿压力带上沿；超过则场景失效
- 从第一根试压到最后一根试压这段区间内，收盘价站上压力带上沿的 K 线超过 `3` 根，场景直接过滤

### 3.1 场景识别抽象

当前代码已经把“窗口切分 / 压力位计算”和“场景识别”拆开：

- 通用窗口与价位计算：`CalculateLevelScenariosByWindows`
- 当前内置场景识别器：`pressureBreakoutDetector`
- 实时信号判断：`CalculateRealtimeScenarioSignalsByWindows`

这样后续新增别的场景时，可以继续复用：

- 同一套 1m / 5m / 15m / 1h K 线
- 同一套滑动窗口
- 同一套压力位/支撑位结果

只需要新增新的 `ScenarioDetector` 实现，不必重写整个框架。

### 4. 回测规则

当前内置方法：`breakout_band_follow`

- 买入点：突破阈值对应的市值
- 下一根 K 线跌破压力带上沿：止损
- 支持硬止损，默认 `-5%`
- 盈利达到触发阈值后，启用动态止损锁盈
- 支持止盈范围扫描，如 `8%-15%`
- 默认总手续费 `1.5%`
- 单个 token 同时最多只保留 `1` 笔持仓：
  - 前一笔未卖出时，后续买点跳过
  - 必须等前一笔卖出后才允许再次买入

### 5. 实时信号

系统已经支持根据实时 K 线动态计算“压力带突破信号”：

- 先用历史 K 线窗口识别出已经成立的试压结构
- 再用当前实时 K 线判断是否已经站上突破阈值
- 满足条件时立即返回信号

当前接口：

- `POST /api/market/birdeye/realtime-breakout-signals`

这个能力适用于多种 K 线周期：

- `1m`
- `5m`
- `15m`
- `1h`

只要传入对应周期的 K 线与实时最新一根 K 线，就能复用同一套信号逻辑。

## 本地开发

### 环境要求

- Go 1.22+
- Node.js 18+
- npm 9+

### 1) 后端启动

复制配置文件：

```bash
cd backend
cp config/config.example.yaml config.yaml
```

至少配置这些项：

- `server.port`
- `database.dsn`
- `birdeye.api_key` 或 `birdeye.api_keys`
- `birdeye.cache_db_path`

启动：

```bash
cd backend
go run ./cmd/server
```

默认健康检查：

```bash
curl http://127.0.0.1:8890/api/health
```

### 2) 前端启动

```bash
cd frontend
npm install
npm run dev
```

默认 Vite 地址：

- `http://127.0.0.1:5173/`

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
GOCACHE=$PWD/.tmp/go-build-cache GOOS=linux GOARCH=amd64 go build -o solana-meme-backtest-linux ./cmd/server
```

前端测试与构建：

```bash
cd frontend
npm test
npm run build
```

## 配置说明

示例配置见：

- `backend/config/config.example.yaml`

当前常用配置项：

- `database.dsn`：业务库 SQLite 地址
- `birdeye.base_url`：Birdeye API 地址
- `birdeye.api_key`：主 API key
- `birdeye.api_keys`：API key 池
- `birdeye.cache_db_path`：Birdeye K 线缓存库
- `datasource.kline_query`：SQL K 线查询模板
- `datasource.token_search_query`：token 搜索 SQL

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

详见：

- `docs/API.md`

常用接口：

- `GET /api/health`
- `GET /api/market/birdeye/klines`
- `GET /api/market/birdeye/support-resistance`
- `POST /api/market/birdeye/realtime-breakout-signals`
- `GET /api/strategy-backtests/methods`
- `POST /api/strategy-backtests/run`

## 开发约束

开发前先看：

- `docs/项目开发规范.md`

当前仓库约束重点：

- 功能变更同步更新中文文档
- 不要擅自加 fallback / 备用数据源
- 回测核心逻辑不能依赖 HTTP / DB 实现
- 前端组件通过 props 接收数据，不直接耦合页面全局状态

## 备注

- 当前仓库已经推送到：`https://github.com/Zhu-JunFeng/memeV2`
- 当前仓库内包含部分构建产物和部署文件；后续如果需要，可以再单独清理 `.gitignore` 和版本库内容
