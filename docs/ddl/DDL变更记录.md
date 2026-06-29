# DDL 变更记录

当前 V2 已统一使用 PostgreSQL；Birdeye K 线缓存、回测结果、交易模块表全部落在 PG 中。

## 001-回测分析表（PostgreSQL）

```sql
CREATE TABLE backtest_sessions (
  id varchar(36) PRIMARY KEY,
  token_address varchar(128) NOT NULL,
  token_symbol varchar(64) NOT NULL DEFAULT '',
  "interval" varchar(32) NOT NULL,
  start_time timestamptz NOT NULL,
  end_time timestamptz NOT NULL,
  created_at timestamptz,
  updated_at timestamptz
);

CREATE INDEX idx_backtest_sessions_token_address ON backtest_sessions (token_address);
CREATE INDEX idx_backtest_sessions_start_time ON backtest_sessions (start_time);
CREATE INDEX idx_backtest_sessions_end_time ON backtest_sessions (end_time);

CREATE TABLE backtest_trade_points (
  id varchar(36) PRIMARY KEY,
  session_id varchar(36) NOT NULL,
  side varchar(16) NOT NULL,
  point_time timestamptz NOT NULL,
  input_price double precision,
  note varchar(512) NOT NULL DEFAULT '',
  matched_kline_time timestamptz,
  matched_price double precision,
  created_at timestamptz
);

CREATE INDEX idx_backtest_trade_points_session_id ON backtest_trade_points (session_id);
CREATE INDEX idx_backtest_trade_points_point_time ON backtest_trade_points (point_time);

CREATE TABLE backtest_trade_results (
  id varchar(36) PRIMARY KEY,
  session_id varchar(36) NOT NULL,
  buy_point_id varchar(36) NOT NULL,
  sell_point_id varchar(36) NOT NULL,
  buy_matched_kline_time timestamptz NOT NULL,
  sell_matched_kline_time timestamptz NOT NULL,
  buy_price double precision NOT NULL,
  sell_price double precision NOT NULL,
  profit double precision NOT NULL,
  profit_rate double precision NOT NULL,
  holding_seconds bigint NOT NULL,
  win boolean NOT NULL,
  created_at timestamptz
);

CREATE INDEX idx_backtest_trade_results_session_id ON backtest_trade_results (session_id);

CREATE TABLE backtest_metric_snapshots (
  id varchar(36) PRIMARY KEY,
  session_id varchar(36) NOT NULL UNIQUE,
  trade_count bigint NOT NULL,
  win_rate double precision NOT NULL,
  total_profit_rate double precision NOT NULL,
  max_drawdown_rate double precision NOT NULL,
  average_holding_seconds bigint NOT NULL,
  created_at timestamptz
);
```

## 002-Birdeye K线缓存表（PostgreSQL）

```sql
CREATE TABLE birdeye_kline_cache (
  token_address varchar(128) NOT NULL,
  "interval" varchar(32) NOT NULL,
  open_time timestamptz NOT NULL,
  close_time timestamptz NOT NULL,
  market_cap_open double precision NOT NULL,
  market_cap_high double precision NOT NULL,
  market_cap_low double precision NOT NULL,
  market_cap_close double precision NOT NULL,
  volume double precision NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (token_address, "interval", open_time)
);

CREATE INDEX idx_birdeye_kline_cache_token_interval_open_time
  ON birdeye_kline_cache (token_address, "interval", open_time);

CREATE TABLE birdeye_kline_cache_ranges (
  token_address varchar(128) NOT NULL,
  "interval" varchar(32) NOT NULL,
  range_start timestamptz NOT NULL,
  range_end timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (token_address, "interval", range_start, range_end)
);

CREATE INDEX idx_birdeye_kline_cache_ranges_lookup
  ON birdeye_kline_cache_ranges (token_address, "interval", range_start, range_end);
```

## 003-交易模块表（PostgreSQL）

```sql
CREATE TABLE trade_accounts (
  id varchar(36) PRIMARY KEY,
  name varchar(64) NOT NULL UNIQUE,
  wallet_address varchar(128) NOT NULL,
  status varchar(16) NOT NULL,
  buy_amount_usd double precision NOT NULL,
  slippage_bps integer NOT NULL,
  priority_fee_lamports bigint NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE trade_signals (
  id varchar(36) PRIMARY KEY,
  signal_id varchar(96) NOT NULL UNIQUE,
  signal_type varchar(16) NOT NULL,
  strategy_code varchar(64) NOT NULL,
  token_address varchar(128) NOT NULL,
  "interval" varchar(32) NOT NULL,
  signal_time timestamptz NOT NULL,
  trigger_price double precision NOT NULL,
  trigger_market_cap double precision NOT NULL,
  reason text NOT NULL,
  raw_payload_json jsonb NOT NULL,
  consume_status varchar(16) NOT NULL,
  created_at timestamptz NOT NULL
);

CREATE INDEX idx_trade_signals_token_address ON trade_signals (token_address);
CREATE INDEX idx_trade_signals_signal_time ON trade_signals (signal_time DESC);

CREATE TABLE trade_orders (
  id varchar(36) PRIMARY KEY,
  account_id varchar(36) NOT NULL,
  signal_id varchar(36) NOT NULL,
  token_address varchar(128) NOT NULL,
  side varchar(16) NOT NULL,
  intent_amount_usd double precision NOT NULL,
  intent_token_amount double precision NOT NULL,
  status varchar(24) NOT NULL,
  jupiter_request_json jsonb,
  jupiter_response_json jsonb,
  submit_tx_hash varchar(128) NOT NULL DEFAULT '',
  confirmed_at timestamptz,
  fail_reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE INDEX idx_trade_orders_account_created_at ON trade_orders (account_id, created_at DESC);
CREATE INDEX idx_trade_orders_signal_id ON trade_orders (signal_id);

CREATE TABLE trade_fills (
  id varchar(36) PRIMARY KEY,
  order_id varchar(36) NOT NULL,
  tx_hash varchar(128) NOT NULL,
  side varchar(16) NOT NULL,
  token_address varchar(128) NOT NULL,
  filled_token_amount double precision NOT NULL,
  filled_quote_amount double precision NOT NULL,
  avg_price double precision NOT NULL,
  fee_amount double precision NOT NULL,
  fee_asset varchar(32) NOT NULL,
  executed_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL
);

CREATE INDEX idx_trade_fills_order_id ON trade_fills (order_id);

CREATE TABLE trade_positions (
  id varchar(36) PRIMARY KEY,
  account_id varchar(36) NOT NULL,
  token_address varchar(128) NOT NULL,
  status varchar(16) NOT NULL,
  open_order_id varchar(36) NOT NULL,
  close_order_id varchar(36) NOT NULL DEFAULT '',
  quantity double precision NOT NULL,
  cost_amount double precision NOT NULL,
  avg_cost_price double precision NOT NULL,
  last_price double precision NOT NULL,
  market_value double precision NOT NULL,
  realized_pnl double precision NOT NULL,
  unrealized_pnl double precision NOT NULL,
  max_profit_rate double precision NOT NULL,
  max_drawdown_amount double precision NOT NULL,
  opened_at timestamptz NOT NULL,
  closed_at timestamptz,
  updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX idx_trade_positions_account_token_open
  ON trade_positions (account_id, token_address)
  WHERE status = 'open';

CREATE INDEX idx_trade_positions_status_updated_at
  ON trade_positions (status, updated_at DESC);

CREATE TABLE trade_order_events (
  id varchar(36) PRIMARY KEY,
  order_id varchar(36) NOT NULL,
  event_type varchar(32) NOT NULL,
  event_time timestamptz NOT NULL,
  detail_json jsonb NOT NULL
);

CREATE INDEX idx_trade_order_events_order_time
  ON trade_order_events (order_id, event_time DESC);
```

## 004-交易模式运行时配置与字段扩展（PostgreSQL）

```sql
CREATE TABLE system_runtime_settings (
  setting_key varchar(64) PRIMARY KEY,
  setting_value text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

ALTER TABLE trade_signals ADD COLUMN IF NOT EXISTS trade_mode varchar(16);
UPDATE trade_signals SET trade_mode = 'live' WHERE COALESCE(trade_mode, '') = '';
ALTER TABLE trade_signals ALTER COLUMN trade_mode SET NOT NULL;

ALTER TABLE trade_orders ADD COLUMN IF NOT EXISTS trade_mode varchar(16);
ALTER TABLE trade_orders ADD COLUMN IF NOT EXISTS execution_channel varchar(32);
UPDATE trade_orders SET trade_mode = 'live' WHERE COALESCE(trade_mode, '') = '';
UPDATE trade_orders SET execution_channel = 'jupiter_live' WHERE COALESCE(execution_channel, '') = '';
ALTER TABLE trade_orders ALTER COLUMN trade_mode SET NOT NULL;
ALTER TABLE trade_orders ALTER COLUMN execution_channel SET NOT NULL;

ALTER TABLE trade_fills ADD COLUMN IF NOT EXISTS trade_mode varchar(16);
ALTER TABLE trade_fills ADD COLUMN IF NOT EXISTS is_simulated boolean;
UPDATE trade_fills SET trade_mode = 'live' WHERE COALESCE(trade_mode, '') = '';
UPDATE trade_fills SET is_simulated = false WHERE is_simulated IS NULL;
ALTER TABLE trade_fills ALTER COLUMN trade_mode SET NOT NULL;
ALTER TABLE trade_fills ALTER COLUMN is_simulated SET NOT NULL;

ALTER TABLE trade_positions ADD COLUMN IF NOT EXISTS trade_mode varchar(16);
UPDATE trade_positions SET trade_mode = 'live' WHERE COALESCE(trade_mode, '') = '';
ALTER TABLE trade_positions ALTER COLUMN trade_mode SET NOT NULL;
```

## 005-Birdeye API Key 池与中文注释（PostgreSQL）

```sql
CREATE TABLE birdeye_api_keys (
  id varchar(36) PRIMARY KEY,
  api_key text NOT NULL UNIQUE,
  key_mask varchar(32) NOT NULL,
  status varchar(16) NOT NULL,
  unavailable_reason text NOT NULL DEFAULT '',
  unavailable_at timestamptz,
  last_successful_used_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE INDEX idx_birdeye_api_keys_status_created_at
  ON birdeye_api_keys (status, created_at ASC);

COMMENT ON TABLE birdeye_api_keys IS 'Birdeye API Key池表';
COMMENT ON COLUMN birdeye_api_keys.id IS 'Key记录ID';
COMMENT ON COLUMN birdeye_api_keys.api_key IS 'Birdeye API Key密文原值';
COMMENT ON COLUMN birdeye_api_keys.key_mask IS '脱敏Key展示值';
COMMENT ON COLUMN birdeye_api_keys.status IS 'Key状态';
COMMENT ON COLUMN birdeye_api_keys.unavailable_reason IS '不可用原因';
COMMENT ON COLUMN birdeye_api_keys.unavailable_at IS '标记不可用时间';
COMMENT ON COLUMN birdeye_api_keys.last_successful_used_at IS '最近成功使用时间';
COMMENT ON COLUMN birdeye_api_keys.created_at IS '创建时间';
COMMENT ON COLUMN birdeye_api_keys.updated_at IS '更新时间';
```

本次迁移同时对既有业务表补充中文表注释和字段注释，覆盖范围：

- `backtest_sessions`、`backtest_trade_points`、`backtest_trade_results`、`backtest_metric_snapshots`
- `birdeye_kline_cache`、`birdeye_kline_cache_ranges`、`birdeye_api_keys`
- `system_runtime_settings`
- `trade_accounts`、`trade_signals`、`trade_orders`、`trade_fills`、`trade_positions`、`trade_order_events`

注释语句随后端启动迁移自动执行，字段含义与当前代码中的业务读写语义保持一致。

## 006-GMGN API Key 池（PostgreSQL）

```sql
CREATE TABLE gmgn_api_keys (
  id varchar(36) PRIMARY KEY,
  api_key text NOT NULL UNIQUE,
  key_mask varchar(32) NOT NULL,
  status varchar(16) NOT NULL,
  unavailable_reason text NOT NULL DEFAULT '',
  unavailable_at timestamptz,
  last_successful_used_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE INDEX idx_gmgn_api_keys_status_created_at
  ON gmgn_api_keys (status, created_at ASC);

COMMENT ON TABLE gmgn_api_keys IS 'GMGN API Key池表';
COMMENT ON COLUMN gmgn_api_keys.id IS 'Key记录ID';
COMMENT ON COLUMN gmgn_api_keys.api_key IS 'GMGN API Key密文原值';
COMMENT ON COLUMN gmgn_api_keys.key_mask IS '脱敏Key展示值';
COMMENT ON COLUMN gmgn_api_keys.status IS 'Key状态';
COMMENT ON COLUMN gmgn_api_keys.unavailable_reason IS '不可用原因';
COMMENT ON COLUMN gmgn_api_keys.unavailable_at IS '标记不可用时间';
COMMENT ON COLUMN gmgn_api_keys.last_successful_used_at IS '最近成功使用时间';
COMMENT ON COLUMN gmgn_api_keys.created_at IS '创建时间';
COMMENT ON COLUMN gmgn_api_keys.updated_at IS '更新时间';
```

GMGN K 线和实时价格请求按 `gmgn_api_keys` 中 `available` 状态 key 轮询；配置文件 `gmgn.api_keys` / `gmgn.api_key` 会在启动时导入表内，新增接口 `POST /api/gmgn/api-keys` 支持在线添加 key。
