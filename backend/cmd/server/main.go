package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"solana-meme-backtest/backend/internal/api"
	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/db"
	"solana-meme-backtest/backend/internal/logger"
	"solana-meme-backtest/backend/internal/repository"
	"solana-meme-backtest/backend/internal/signal"
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
		logg.Fatal().Err(err).Msg("连接数据库失败")
	}
	cacheDB, err := db.OpenSQLite(cfg.Birdeye.CacheDBPath)
	if err != nil {
		logg.Fatal().Err(err).Str("path", cfg.Birdeye.CacheDBPath).Msg("打开 Birdeye sqlite cache 失败")
	}
	source := datasource.NewSQLDataSource(database, cfg.Datasource.KlineQuery, cfg.Datasource.TokenSearchQuery)
	dbBarSource := datasource.NewDBBarDataSource(database)
	dbTradePointSource := datasource.NewDBTradePointDataSource(database)
	birdeyeUpstream := datasource.NewBirdeyeDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain)
	birdeyeSource := datasource.NewBirdeyeCachedDataSource(cacheDB, birdeyeUpstream)
	tradePointSource := datasource.NewBirdeyeTradePointDataSource(cfg.Birdeye.BaseURL, cfg.Birdeye.APIKeys, cfg.Birdeye.Chain, cfg.Birdeye.TradeMaxPages)
	bitqueryTradePointSource := datasource.NewBitqueryTradePointDataSource(cfg.Bitquery.BaseURL, cfg.Bitquery.APIKey)
	repo := repository.NewBacktestRepository(database)
	backtestService := backtest.NewService(source, dbBarSource, birdeyeSource, tradePointSource, bitqueryTradePointSource, dbTradePointSource, source, repo)
	var publisher signal.Publisher
	if cfg.Redis.Enabled && cfg.Redis.Addr != "" {
		publisher = signal.NewRedisPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB, cfg.Redis.Channel)
	}
	signalService := signal.NewService(birdeyeSource, publisher)
	router := api.NewRouter(backtestService, signalService)
	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	logg.Info().Str("addr", addr).Msg("回测服务启动")
	if err := router.Run(addr); err != nil {
		logg.Fatal().Err(err).Msg("回测服务退出")
	}
}
