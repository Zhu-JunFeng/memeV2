# DDL 变更记录

## 001-回测分析持久化表

```sql
CREATE TABLE backtest_sessions (
  id varchar(36) PRIMARY KEY,
  token_address varchar(128) NOT NULL,
  token_symbol varchar(64),
  `interval` varchar(32) NOT NULL,
  start_time datetime(3) NOT NULL,
  end_time datetime(3) NOT NULL,
  created_at datetime(3),
  updated_at datetime(3),
  INDEX idx_backtest_sessions_token_address (token_address),
  INDEX idx_backtest_sessions_start_time (start_time),
  INDEX idx_backtest_sessions_end_time (end_time)
);

CREATE TABLE backtest_trade_points (
  id varchar(36) PRIMARY KEY,
  session_id varchar(36) NOT NULL,
  side varchar(16) NOT NULL,
  point_time datetime(3) NOT NULL,
  input_price double,
  note varchar(512),
  matched_kline_time datetime(3),
  matched_price double,
  created_at datetime(3),
  INDEX idx_backtest_trade_points_session_id (session_id),
  INDEX idx_backtest_trade_points_point_time (point_time)
);

CREATE TABLE backtest_trade_results (
  id varchar(36) PRIMARY KEY,
  session_id varchar(36) NOT NULL,
  buy_point_id varchar(36) NOT NULL,
  sell_point_id varchar(36) NOT NULL,
  buy_matched_kline_time datetime(3) NOT NULL,
  sell_matched_kline_time datetime(3) NOT NULL,
  buy_price double NOT NULL,
  sell_price double NOT NULL,
  profit double NOT NULL,
  profit_rate double NOT NULL,
  holding_seconds bigint NOT NULL,
  win boolean NOT NULL,
  created_at datetime(3),
  INDEX idx_backtest_trade_results_session_id (session_id)
);

CREATE TABLE backtest_metric_snapshots (
  id varchar(36) PRIMARY KEY,
  session_id varchar(36) NOT NULL UNIQUE,
  trade_count bigint NOT NULL,
  win_rate double NOT NULL,
  total_profit_rate double NOT NULL,
  max_drawdown_rate double NOT NULL,
  average_holding_seconds bigint NOT NULL,
  created_at datetime(3)
);
```

## 002-Birdeye K线 sqlite cache 表

本地 sqlite cache 新增两张表，用于缓存 Birdeye K 线和已覆盖的时间范围。

```sql
CREATE TABLE birdeye_kline_cache (
  token_address TEXT NOT NULL,
  interval TEXT NOT NULL,
  open_time TEXT NOT NULL,
  close_time TEXT NOT NULL,
  market_cap_open REAL NOT NULL,
  market_cap_high REAL NOT NULL,
  market_cap_low REAL NOT NULL,
  market_cap_close REAL NOT NULL,
  volume REAL NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (token_address, interval, open_time)
);

CREATE INDEX idx_birdeye_kline_cache_token_interval_open_time
  ON birdeye_kline_cache (token_address, interval, open_time);

CREATE TABLE birdeye_kline_cache_ranges (
  token_address TEXT NOT NULL,
  interval TEXT NOT NULL,
  range_start TEXT NOT NULL,
  range_end TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (token_address, interval, range_start, range_end)
);

CREATE INDEX idx_birdeye_kline_cache_ranges_lookup
  ON birdeye_kline_cache_ranges (token_address, interval, range_start, range_end);
```
