package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server     ServerConfig
	Database   DatabaseConfig
	Datasource DatasourceConfig
	Birdeye    BirdeyeConfig
	GMGN       GMGNConfig
	Bitquery   BitqueryConfig
	Redis      RedisConfig
	Signal     SignalConfig
	Trade      TradeConfig
}

type ServerConfig struct {
	Port string
	Mode string
}

type DatabaseConfig struct {
	DSN         string
	AutoMigrate bool
}

type DatasourceConfig struct {
	KlineQuery       string
	TokenSearchQuery string
	KlineSource      string
}

type BirdeyeConfig struct {
	BaseURL       string
	APIKey        string
	APIKeys       []string
	Chain         string
	TradeMaxPages int
}

type GMGNConfig struct {
	BaseURL string
	APIKey  string
	APIKeys []string
	Chain   string
	MaxQPS  float64
}

type BitqueryConfig struct {
	BaseURL string
	APIKey  string
}

type RedisConfig struct {
	Addr            string
	Password        string
	DB              int
	Channel         string
	ConsumerChannel string
	Enabled         bool
}

type SignalConfig struct {
	CandidateMonitorEnabled bool
	CandidateChannel        string
	PollIntervalSeconds     int
	Interval                string
	MinMarketCap            float64
	LookbackBars            int
	RedisKeyPrefix          string
}

type TradeConfig struct {
	Enabled           bool
	SignalConsumer    bool
	PriceSyncEnabled  bool
	PriceSyncInterval int
	WalletAddress     string
	WalletPrivateKey  string
	AccountName       string
	BuyAmountUSD      float64
	SlippageBPS       int
	PriorityFee       int64
	SolanaRPCURL      string
	DexScreener       DexScreenerConfig
	Jupiter           JupiterConfig
	PriceSource       string
}

type DexScreenerConfig struct {
	BaseURL string
}

type JupiterConfig struct {
	BaseURL string
	APIKey  string
}

func Load() (Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./config")
	v.AddConfigPath(".")
	v.SetEnvPrefix("BACKTEST")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("server.port", "8890")
	v.SetDefault("server.mode", "debug")
	v.SetDefault("database.auto_migrate", false)
	v.SetDefault("datasource.kline_source", "gmgn")
	v.SetDefault("birdeye.base_url", "https://public-api.birdeye.so")
	v.SetDefault("birdeye.chain", "solana")
	v.SetDefault("birdeye.trade_max_pages", 1)
	v.SetDefault("gmgn.base_url", "https://openapi.gmgn.ai")
	v.SetDefault("gmgn.chain", "sol")
	v.SetDefault("gmgn.max_qps", 8.0)
	v.SetDefault("bitquery.base_url", "https://streaming.bitquery.io/graphql")
	v.SetDefault("redis.channel", "solana:meme:signals:pressure_breakout")
	v.SetDefault("redis.consumer_channel", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.enabled", false)
	v.SetDefault("signal.candidate_monitor_enabled", false)
	v.SetDefault("signal.candidate_channel", "solana_scalper:candidate_pool")
	v.SetDefault("signal.poll_interval_seconds", 2)
	v.SetDefault("signal.interval", "1m")
	v.SetDefault("signal.min_market_cap", 0)
	v.SetDefault("signal.lookback_bars", 120)
	v.SetDefault("signal.redis_key_prefix", "solana_meme_v2:signal_monitor")
	v.SetDefault("trade.enabled", false)
	v.SetDefault("trade.signal_consumer", false)
	v.SetDefault("trade.price_sync_enabled", false)
	v.SetDefault("trade.price_sync_interval", 15)
	v.SetDefault("trade.account_name", "default")
	v.SetDefault("trade.buy_amount_usd", 10)
	v.SetDefault("trade.slippage_bps", 500)
	v.SetDefault("trade.priority_fee", 0)
	v.SetDefault("trade.solana_rpc_url", "https://api.mainnet-beta.solana.com")
	v.SetDefault("trade.price_source", "gmgn")
	v.SetDefault("trade.dexscreener.base_url", "https://api.dexscreener.com")
	v.SetDefault("trade.jupiter.base_url", "https://lite-api.jup.ag")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, err
		}
	}

	return Config{
		Server: ServerConfig{
			Port: v.GetString("server.port"),
			Mode: v.GetString("server.mode"),
		},
		Database: DatabaseConfig{
			DSN:         v.GetString("database.dsn"),
			AutoMigrate: v.GetBool("database.auto_migrate"),
		},
		Datasource: DatasourceConfig{
			KlineQuery:       v.GetString("datasource.kline_query"),
			TokenSearchQuery: v.GetString("datasource.token_search_query"),
			KlineSource:      normalizeSourceName(v.GetString("datasource.kline_source"), "gmgn"),
		},
		Birdeye: BirdeyeConfig{
			BaseURL:       v.GetString("birdeye.base_url"),
			APIKey:        v.GetString("birdeye.api_key"),
			APIKeys:       normalizeAPIKeys(v.GetStringSlice("birdeye.api_keys"), v.GetString("birdeye.api_key")),
			Chain:         v.GetString("birdeye.chain"),
			TradeMaxPages: v.GetInt("birdeye.trade_max_pages"),
		},
		GMGN: GMGNConfig{
			BaseURL: v.GetString("gmgn.base_url"),
			APIKey:  v.GetString("gmgn.api_key"),
			APIKeys: normalizeAPIKeys(v.GetStringSlice("gmgn.api_keys"), v.GetString("gmgn.api_key")),
			Chain:   v.GetString("gmgn.chain"),
			MaxQPS:  v.GetFloat64("gmgn.max_qps"),
		},
		Bitquery: BitqueryConfig{
			BaseURL: v.GetString("bitquery.base_url"),
			APIKey:  v.GetString("bitquery.api_key"),
		},
		Redis: RedisConfig{
			Addr:            v.GetString("redis.addr"),
			Password:        v.GetString("redis.password"),
			DB:              v.GetInt("redis.db"),
			Channel:         v.GetString("redis.channel"),
			ConsumerChannel: v.GetString("redis.consumer_channel"),
			Enabled:         v.GetBool("redis.enabled"),
		},
		Signal: SignalConfig{
			CandidateMonitorEnabled: v.GetBool("signal.candidate_monitor_enabled"),
			CandidateChannel:        v.GetString("signal.candidate_channel"),
			PollIntervalSeconds:     v.GetInt("signal.poll_interval_seconds"),
			Interval:                v.GetString("signal.interval"),
			MinMarketCap:            v.GetFloat64("signal.min_market_cap"),
			LookbackBars:            v.GetInt("signal.lookback_bars"),
			RedisKeyPrefix:          v.GetString("signal.redis_key_prefix"),
		},
		Trade: TradeConfig{
			Enabled:           v.GetBool("trade.enabled"),
			SignalConsumer:    v.GetBool("trade.signal_consumer"),
			PriceSyncEnabled:  v.GetBool("trade.price_sync_enabled"),
			PriceSyncInterval: v.GetInt("trade.price_sync_interval"),
			WalletAddress:     v.GetString("trade.wallet_address"),
			WalletPrivateKey:  v.GetString("trade.wallet_private_key"),
			AccountName:       v.GetString("trade.account_name"),
			BuyAmountUSD:      v.GetFloat64("trade.buy_amount_usd"),
			SlippageBPS:       v.GetInt("trade.slippage_bps"),
			PriorityFee:       v.GetInt64("trade.priority_fee"),
			SolanaRPCURL:      v.GetString("trade.solana_rpc_url"),
			PriceSource:       normalizeSourceName(v.GetString("trade.price_source"), "gmgn"),
			DexScreener: DexScreenerConfig{
				BaseURL: v.GetString("trade.dexscreener.base_url"),
			},
			Jupiter: JupiterConfig{
				BaseURL: v.GetString("trade.jupiter.base_url"),
				APIKey:  v.GetString("trade.jupiter.api_key"),
			},
		},
	}, nil
}

func normalizeAPIKeys(keys []string, fallback string) []string {
	normalized := make([]string, 0, len(keys)+1)
	seen := make(map[string]struct{}, len(keys)+1)
	appendKey := func(value string) {
		for _, item := range strings.Split(value, ",") {
			key := strings.TrimSpace(item)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, key)
		}
	}
	for _, value := range keys {
		appendKey(value)
	}
	appendKey(fallback)
	return normalized
}

func normalizeSourceName(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}
