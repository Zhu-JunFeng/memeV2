package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/api"
	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/db"
	"solana-meme-backtest/backend/internal/eventbus"
	"solana-meme-backtest/backend/internal/logger"
	"solana-meme-backtest/backend/internal/repository"
	"solana-meme-backtest/backend/internal/signal"
	"solana-meme-backtest/backend/internal/trade"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	logg := logger.New()
	gin.SetMode(cfg.Server.Mode)
	database, err := db.Open(cfg.Database.DSN, cfg.Database.AutoMigrate)
	if err != nil {
		logg.Fatal().Err(err).Msg("连接 PostgreSQL 失败")
	}
	source := datasource.NewSQLDataSource(database, cfg.Datasource.KlineQuery, cfg.Datasource.TokenSearchQuery)
	dbBarSource := datasource.NewDBBarDataSource(database)
	dbTradePointSource := datasource.NewDBTradePointDataSource(database)
	systemKlineStore := datasource.NewSystemKlineStore(database)
	systemKlineStore.Start(context.Background())
	birdeyeKeyRepo := repository.NewBirdeyeAPIKeyRepository(database)
	if err := birdeyeKeyRepo.EnsureConfigKeys(context.Background(), cfg.Birdeye.APIKeys); err != nil {
		logg.Fatal().Err(err).Msg("初始化 Birdeye API Key 池失败")
	}
	birdeyeUpstream := datasource.NewBirdeyeDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain).WithKeyPool(birdeyeKeyRepo)
	birdeyeSource := datasource.NewBirdeyeCachedDataSource(database, birdeyeUpstream)
	gmgnSource := datasource.NewGMGNDataSource(cfg.GMGN.BaseURL, cfg.GMGN.APIKey, cfg.GMGN.Chain, cfg.GMGN.MaxQPS)
	supplyProvider := datasource.NewSolanaRPCSupplyProvider(cfg.Trade.SolanaRPCURL)
	events := eventbus.NewBroker()
	primaryKlineSource, err := selectKlineSource(cfg.Datasource.KlineSource, source, dbBarSource, birdeyeSource, gmgnSource, systemKlineStore)
	if err != nil {
		logg.Fatal().Err(err).Msg("K 线数据源配置错误")
	}
	primaryRealtimeKlineSource, err := selectKlineSource(cfg.Datasource.KlineSource, source, dbBarSource, birdeyeUpstream, gmgnSource, systemKlineStore)
	if err != nil {
		logg.Fatal().Err(err).Msg("实时 K 线数据源配置错误")
	}
	tradePointSource := datasource.NewBirdeyeTradePointDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain, cfg.Birdeye.TradeMaxPages).WithKeyPool(birdeyeKeyRepo)
	bitqueryTradePointSource := datasource.NewBitqueryTradePointDataSource(cfg.Bitquery.BaseURL, cfg.Bitquery.APIKey)
	backtestRepo := repository.NewBacktestRepository(database)
	tradeRepo := repository.NewTradeRepository(database)
	backtestService := backtest.NewService(primaryKlineSource, dbBarSource, birdeyeSource, tradePointSource, bitqueryTradePointSource, dbTradePointSource, source, backtestRepo,
		backtest.WithDefaultKlineSource(cfg.Datasource.KlineSource),
		backtest.WithKlineSource("sql", source),
		backtest.WithKlineSource("db", dbBarSource),
		backtest.WithKlineSource("birdeye", birdeyeSource),
		backtest.WithKlineSource("gmgn", gmgnSource),
		backtest.WithKlineSource("system", systemKlineStore),
	)
	var publisher signal.Publisher
	var redisClient *redis.Client
	if cfg.Redis.Enabled && cfg.Redis.Addr != "" {
		publisher = signal.NewRedisPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB, cfg.Redis.Channel)
		redisClient = redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	}
	signalService := signal.NewService(primaryKlineSource, publisher,
		signal.WithDefaultKlineSource(cfg.Datasource.KlineSource),
		signal.WithKlineSource("sql", source),
		signal.WithKlineSource("db", dbBarSource),
		signal.WithKlineSource("birdeye", birdeyeSource),
		signal.WithKlineSource("gmgn", gmgnSource),
		signal.WithKlineSource("system", systemKlineStore),
	)
	var candidateMonitor *signal.CandidateMonitor
	if cfg.Signal.CandidateMonitorEnabled && redisClient != nil {
		candidateMonitor = signal.NewCandidateMonitor(redisClient, primaryRealtimeKlineSource, publisher, signal.CandidateMonitorConfig{
			Enabled:          cfg.Signal.CandidateMonitorEnabled,
			CandidateChannel: cfg.Signal.CandidateChannel,
			PollInterval:     time.Duration(cfg.Signal.PollIntervalSeconds) * time.Second,
			Interval:         cfg.Signal.Interval,
			MinMarketCap:     cfg.Signal.MinMarketCap,
			LookbackBars:     cfg.Signal.LookbackBars,
			RedisKeyPrefix:   cfg.Signal.RedisKeyPrefix,
			LevelOptions:     backtest.DefaultLevelOptions(),
			BreakoutFollow:   backtest.DefaultBreakoutBandFollowConfig(),
			SupplyProvider:   supplyProvider,
			SystemKlines:     systemKlineStore,
			EventBus:         events,
		})
		candidateMonitor.Start(context.Background())
	}
	priceSource, err := selectPriceSource(cfg.Trade.PriceSource, datasource.NewDexScreenerPriceSource(cfg.Trade.DexScreener.BaseURL), gmgnSource)
	if err != nil {
		logg.Fatal().Err(err).Msg("价格数据源配置错误")
	}
	jupiterExecutor, err := trade.NewJupiterExecutor(cfg.Trade, priceSource)
	if err != nil && cfg.Trade.Enabled {
		logg.Fatal().Err(err).Msg("初始化 Jupiter 执行器失败")
	}
	tradeService, err := trade.NewService(context.Background(), cfg.Trade, tradeRepo, jupiterExecutor, priceSource, trade.WithEventBus(events))
	if err != nil {
		logg.Fatal().Err(err).Msg("初始化交易模块失败")
	}
	if tradeService.Enabled() {
		consumerChannel := cfg.Redis.ConsumerChannel
		if consumerChannel == "" {
			consumerChannel = cfg.Redis.Channel
		}
		worker := trade.NewWorker(tradeService, redisClient, consumerChannel)
		if cfg.Trade.SignalConsumer {
			worker.StartSignalConsumer(context.Background())
		}
		if cfg.Trade.PriceSyncEnabled {
			interval := time.Duration(cfg.Trade.PriceSyncInterval) * time.Second
			worker.StartPriceSync(context.Background(), interval)
		}
	}
	router := api.NewRouter(backtestService, signalService, tradeService, candidateMonitor, birdeyeKeyRepo, events)
	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	logg.Info().Str("addr", addr).Msg("回测服务启动")
	if err := router.Run(addr); err != nil {
		logg.Fatal().Err(err).Msg("回测服务退出")
	}
}

func selectKlineSource(name string, sqlSource datasource.KlineDataSource, dbSource datasource.KlineDataSource, birdeyeSource datasource.KlineDataSource, gmgnSource datasource.KlineDataSource, systemSource datasource.KlineDataSource) (datasource.KlineDataSource, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "gmgn":
		return gmgnSource, nil
	case "birdeye":
		return birdeyeSource, nil
	case "sql":
		return sqlSource, nil
	case "db":
		return dbSource, nil
	case "system":
		return systemSource, nil
	default:
		return nil, fmt.Errorf("%w: %s", datasource.ErrUnsupportedKlineSource, name)
	}
}

func selectPriceSource(name string, dexScreenerSource datasource.TokenPriceProvider, gmgnSource datasource.TokenPriceProvider) (datasource.TokenPriceProvider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "gmgn":
		return gmgnSource, nil
	case "dexscreener":
		return dexScreenerSource, nil
	default:
		return nil, fmt.Errorf("%w: %s", datasource.ErrUnsupportedPriceSource, name)
	}
}
