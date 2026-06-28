package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/api"
	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/db"
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
	birdeyeUpstream := datasource.NewBirdeyeDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain)
	birdeyeSource := datasource.NewBirdeyeCachedDataSource(database, birdeyeUpstream)
	tradePointSource := datasource.NewBirdeyeTradePointDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain, cfg.Birdeye.TradeMaxPages)
	bitqueryTradePointSource := datasource.NewBitqueryTradePointDataSource(cfg.Bitquery.BaseURL, cfg.Bitquery.APIKey)
	backtestRepo := repository.NewBacktestRepository(database)
	tradeRepo := repository.NewTradeRepository(database)
	backtestService := backtest.NewService(source, dbBarSource, birdeyeSource, tradePointSource, bitqueryTradePointSource, dbTradePointSource, source, backtestRepo)
	var publisher signal.Publisher
	var redisClient *redis.Client
	if cfg.Redis.Enabled && cfg.Redis.Addr != "" {
		publisher = signal.NewRedisPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB, cfg.Redis.Channel)
		redisClient = redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	}
	signalService := signal.NewService(birdeyeSource, publisher)
	var candidateMonitor *signal.CandidateMonitor
	if cfg.Signal.CandidateMonitorEnabled && redisClient != nil {
		candidateMonitor = signal.NewCandidateMonitor(redisClient, birdeyeUpstream, publisher, signal.CandidateMonitorConfig{
			Enabled:          cfg.Signal.CandidateMonitorEnabled,
			CandidateChannel: cfg.Signal.CandidateChannel,
			PollInterval:     time.Duration(cfg.Signal.PollIntervalSeconds) * time.Second,
			Interval:         cfg.Signal.Interval,
			MinMarketCap:     cfg.Signal.MinMarketCap,
			LookbackBars:     cfg.Signal.LookbackBars,
			RedisKeyPrefix:   cfg.Signal.RedisKeyPrefix,
			LevelOptions:     backtest.DefaultLevelOptions(),
			BreakoutFollow:   backtest.DefaultBreakoutBandFollowConfig(),
		})
		candidateMonitor.Start(context.Background())
	}
	priceSource := datasource.NewDexScreenerPriceSource(cfg.Trade.DexScreener.BaseURL)
	jupiterExecutor, err := trade.NewJupiterExecutor(cfg.Trade, priceSource)
	if err != nil && cfg.Trade.Enabled {
		logg.Fatal().Err(err).Msg("初始化 Jupiter 执行器失败")
	}
	tradeService, err := trade.NewService(context.Background(), cfg.Trade, tradeRepo, jupiterExecutor, priceSource)
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
	router := api.NewRouter(backtestService, signalService, tradeService, candidateMonitor)
	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	logg.Info().Str("addr", addr).Msg("回测服务启动")
	if err := router.Run(addr); err != nil {
		logg.Fatal().Err(err).Msg("回测服务退出")
	}
}
