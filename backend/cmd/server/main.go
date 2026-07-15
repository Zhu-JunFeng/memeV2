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
	"solana-meme-backtest/backend/internal/integration/gmgnkeys"
	"solana-meme-backtest/backend/internal/integration/gmgnprojects"
	"solana-meme-backtest/backend/internal/integration/telegram"
	"solana-meme-backtest/backend/internal/integration/xxyy"
	"solana-meme-backtest/backend/internal/logger"
	"solana-meme-backtest/backend/internal/repository"
	"solana-meme-backtest/backend/internal/runtimealert"
	"solana-meme-backtest/backend/internal/runtimeconfig"
	"solana-meme-backtest/backend/internal/signal"
	"solana-meme-backtest/backend/internal/trade"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	logg := logger.New()
	appCtx := context.Background()
	var telegramClient *telegram.Client
	var alertNotifier runtimealert.Notifier
	if cfg.Telegram.Enabled {
		telegramClient = telegram.NewClient(cfg.Telegram.BotToken, cfg.Telegram.ChatID, nil)
		alertNotifier = telegramClient
	}
	alertMonitor := runtimealert.New(runtimealert.Config{
		Enabled:                    cfg.Alert.Enabled,
		LatencyThreshold:           time.Duration(cfg.Alert.LatencyThresholdMS) * time.Millisecond,
		ConsecutiveFailures:        cfg.Alert.ConsecutiveFailures,
		ConsecutiveHighLatency:     cfg.Alert.ConsecutiveHighLatency,
		RecoverySuccesses:          cfg.Alert.RecoverySuccesses,
		Cooldown:                   time.Duration(cfg.Alert.CooldownSeconds) * time.Second,
		ResourceCheckInterval:      time.Duration(cfg.Alert.ResourceCheckInterval) * time.Second,
		ResourceConsecutiveSamples: cfg.Alert.ResourceConsecutiveSamples,
		CPUThresholdPercent:        cfg.Alert.CPUThresholdPercent,
		MemoryThresholdPercent:     cfg.Alert.MemoryThresholdPercent,
	}, alertNotifier, nil)
	gin.SetMode(cfg.Server.Mode)
	database, err := db.Open(cfg.Database.DSN, cfg.Database.AutoMigrate)
	if err != nil {
		logg.Fatal().Err(err).Msg("连接 PostgreSQL 失败")
	}
	source := datasource.NewSQLDataSource(database, cfg.Datasource.KlineQuery, cfg.Datasource.TokenSearchQuery)
	dbBarSource := datasource.NewDBBarDataSource(database)
	dbTradePointSource := datasource.NewDBTradePointDataSource(database)
	systemKlineStore := datasource.NewSystemKlineStore(database)
	systemKlineStore.Start(appCtx)
	birdeyeKeyRepo := repository.NewBirdeyeAPIKeyRepository(database)
	if err := birdeyeKeyRepo.EnsureConfigKeys(context.Background(), cfg.Birdeye.APIKeys); err != nil {
		logg.Fatal().Err(err).Msg("初始化 Birdeye API Key 池失败")
	}
	gmgnKeyRepo := repository.NewGMGNAPIKeyRepository(database)
	if err := gmgnKeyRepo.EnsureConfigKeys(context.Background(), cfg.GMGN.APIKeys); err != nil {
		logg.Fatal().Err(err).Msg("初始化 GMGN API Key 池失败")
	}
	alertMonitor.WithAPIKeyPool("GMGN API Key 池", gmgnKeyRepo)
	alertMonitor.Start(appCtx)
	gmgnKeyScheduler := gmgnkeys.NewScheduler(gmgnKeyRepo, nil, cfg.GMGN.MaxQPS)
	birdeyeUpstream := datasource.NewBirdeyeDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain).WithKeyPool(birdeyeKeyRepo).WithHTTPObserver(alertMonitor)
	birdeyeSource := datasource.NewBirdeyeCachedDataSource(database, birdeyeUpstream)
	gmgnSource := datasource.NewGMGNDataSourceWithKeys(cfg.GMGN.BaseURL, nil, cfg.GMGN.Chain, cfg.GMGN.MaxQPS).WithKeyScheduler(gmgnKeyScheduler).WithHTTPObserver(alertMonitor)
	supplyProvider := datasource.NewSolanaRPCSupplyProvider(cfg.Trade.SolanaRPCURL).WithHTTPObserver(alertMonitor)
	events := eventbus.NewBroker()
	primaryKlineSource, err := selectKlineSource(cfg.Datasource.KlineSource, source, dbBarSource, birdeyeSource, gmgnSource, systemKlineStore)
	if err != nil {
		logg.Fatal().Err(err).Msg("K 线数据源配置错误")
	}
	tradePointSource := datasource.NewBirdeyeTradePointDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain, cfg.Birdeye.TradeMaxPages).WithKeyPool(birdeyeKeyRepo).WithHTTPObserver(alertMonitor)
	bitqueryTradePointSource := datasource.NewBitqueryTradePointDataSource(cfg.Bitquery.BaseURL, cfg.Bitquery.APIKey).WithHTTPObserver(alertMonitor)
	backtestRepo := repository.NewBacktestRepository(database)
	tradeRepo := repository.NewTradeRepository(database)
	runtimeSettingsRepo := repository.NewRuntimeSettingsRepository(database)
	runtimeControl, err := runtimeconfig.New(appCtx, runtimeSettingsRepo, runtimeconfig.State{
		CAMonitoringEnabled:   cfg.Signal.CandidateMonitorEnabled,
		TradeExecutionEnabled: cfg.Trade.SignalConsumer,
	})
	if err != nil {
		logg.Fatal().Err(err).Msg("初始化运行时开关失败")
	}
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
	if redisClient != nil {
		candidateMonitor = signal.NewCandidateMonitor(redisClient, gmgnSource, publisher, signal.CandidateMonitorConfig{
			Enabled:          true,
			CandidateChannel: cfg.Signal.CandidateChannel,
			PollInterval:     time.Duration(cfg.Signal.PollIntervalSeconds) * time.Second,
			Interval:         cfg.Signal.Interval,
			MinMarketCap:     cfg.Signal.MinMarketCap,
			LookbackBars:     cfg.Signal.LookbackBars,
			RedisKeyPrefix:   cfg.Signal.RedisKeyPrefix,
			LevelOptions:     backtest.DefaultLevelOptions(),
			BreakoutFollow:   backtest.DefaultBreakoutBandFollowConfig(),
			SupplyProvider:   supplyProvider,
			KlineSource:      gmgnSource,
			SystemKlines:     systemKlineStore,
			EventBus:         events,
			SignalStatus:     tradeRepo,
			RuntimeEnabled:   runtimeControl.CAMonitoringEnabled,
		})
		candidateMonitor.Start(appCtx)
		if cfg.XXYY.Enabled {
			xxyyClient := xxyy.NewClient(cfg.XXYY.BaseURL, cfg.XXYY.APIKey, nil).WithHTTPObserver(alertMonitor)
			gmgnProjectClient := gmgnprojects.NewClient(cfg.GMGN.BaseURL, "", nil).WithKeyScheduler(gmgnKeyScheduler).WithHTTPObserver(alertMonitor)
			signal.NewProjectCandidatePoller(xxyyClient, gmgnProjectClient, candidateMonitor, time.Duration(cfg.XXYY.PollIntervalSeconds)*time.Second).Start(appCtx)
		}
	}
	priceSource, err := selectPriceSource(cfg.Trade.PriceSource, datasource.NewDexScreenerPriceSource(cfg.Trade.DexScreener.BaseURL).WithHTTPObserver(alertMonitor), gmgnSource)
	if err != nil {
		logg.Fatal().Err(err).Msg("价格数据源配置错误")
	}
	jupiterExecutor, err := trade.NewJupiterExecutor(cfg.Trade, priceSource)
	if err != nil && cfg.Trade.Enabled {
		logg.Fatal().Err(err).Msg("初始化 Jupiter 执行器失败")
	}
	if jupiterExecutor != nil {
		jupiterExecutor.WithHTTPObserver(alertMonitor)
	}
	var tradeNotifier trade.Notifier
	if telegramClient != nil {
		tradeNotifier = telegramClient
	}
	tradeOptions := []trade.ServiceOption{trade.WithEventBus(events), trade.WithSupplyProvider(supplyProvider), trade.WithWalletBalanceProvider(supplyProvider), trade.WithNotifier(tradeNotifier)}
	if redisClient != nil {
		tradeOptions = append(tradeOptions, trade.WithPositionStore(trade.NewRedisPositionStore(redisClient, "")))
	}
	tradeService, err := trade.NewService(appCtx, cfg.Trade, tradeRepo, jupiterExecutor, priceSource, tradeOptions...)
	if err != nil {
		logg.Fatal().Err(err).Msg("初始化交易模块失败")
	}
	if tradeService.Enabled() {
		consumerChannel := cfg.Redis.ConsumerChannel
		if consumerChannel == "" {
			consumerChannel = cfg.Redis.Channel
		}
		worker := trade.NewWorker(tradeService, redisClient, consumerChannel, runtimeControl.TradeExecutionEnabled)
		worker.StartSignalConsumer(appCtx)
		if cfg.Trade.PriceSyncEnabled {
			interval := time.Duration(cfg.Trade.PriceSyncInterval) * time.Second
			worker.StartPriceSync(appCtx, interval)
		}
	}
	router := api.NewRouter(backtestService, signalService, tradeService, candidateMonitor, birdeyeKeyRepo, gmgnKeyRepo, events, runtimeControl)
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
