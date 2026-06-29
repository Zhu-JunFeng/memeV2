package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/eventbus"
	"solana-meme-backtest/backend/internal/model"
	"solana-meme-backtest/backend/internal/response"
	"solana-meme-backtest/backend/internal/signal"
	"solana-meme-backtest/backend/internal/trade"
)

type Handler struct {
	backtestService  *backtest.Service
	signalService    *signal.Service
	tradeService     *trade.Service
	candidateMonitor *signal.CandidateMonitor
	birdeyeKeyStore  birdeyeAPIKeyStore
	eventBus         *eventbus.Broker
}

type birdeyeAPIKeyStore interface {
	AddKey(ctx context.Context, apiKey string) (model.BirdeyeAPIKey, error)
}

func NewRouter(backtestService *backtest.Service, signalService *signal.Service, tradeService *trade.Service, candidateMonitor *signal.CandidateMonitor, birdeyeKeyStore birdeyeAPIKeyStore, bus *eventbus.Broker) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	h := &Handler{backtestService: backtestService, signalService: signalService, tradeService: tradeService, candidateMonitor: candidateMonitor, birdeyeKeyStore: birdeyeKeyStore, eventBus: bus}
	api := r.Group("/api")
	api.GET("/health", h.health)
	api.GET("/tokens/search", h.searchTokens)
	api.GET("/market/klines", h.getKlines)
	api.GET("/market/support-resistance", h.getSupportResistance)
	api.POST("/market/realtime-breakout-signals", h.getRealtimeSignals)
	api.GET("/market/birdeye/klines", h.getBirdeyeKlines)
	api.GET("/market/birdeye/support-resistance", h.getBirdeyeSupportResistance)
	api.POST("/market/birdeye/realtime-breakout-signals", h.getBirdeyeRealtimeSignals)
	api.GET("/market/gmgn/klines", h.getGMGNKlines)
	api.GET("/market/gmgn/support-resistance", h.getGMGNSupportResistance)
	api.POST("/market/gmgn/realtime-breakout-signals", h.getGMGNRealtimeSignals)
	api.POST("/birdeye/api-keys", h.createBirdeyeAPIKey)
	api.GET("/signal/candidate-monitor", h.listCandidateMonitor)
	api.POST("/signal/candidate-monitor", h.addCandidateMonitor)
	api.GET("/signal/candidate-monitor/stream", h.streamCandidateMonitor)
	api.GET("/market/db/support-resistance", h.getDBSupportResistance)
	api.GET("/strategy-backtests/methods", h.listStrategyMethods)
	api.POST("/strategy-backtests/run", h.runStrategyBacktest)
	api.POST("/backtests", h.createBacktest)
	api.GET("/backtests", h.listBacktests)
	api.GET("/backtests/:id", h.getBacktest)
	api.GET("/trade/accounts", h.listTradeAccounts)
	api.GET("/trade/runtime", h.getTradeRuntime)
	api.PUT("/trade/runtime", h.updateTradeRuntime)
	api.GET("/trade/summary", h.listTradeSummary)
	api.GET("/trade/signals", h.listTradeSignals)
	api.GET("/trade/signals/stream", h.streamTradeSignals)
	api.GET("/trade/orders", h.listTradeOrders)
	api.GET("/trade/orders/stream", h.streamTradeOrders)
	api.GET("/trade/orders/:id", h.getTradeOrder)
	api.POST("/trade/orders/:id/retry", h.retryTradeOrder)
	api.GET("/trade/positions", h.listTradePositions)
	api.GET("/trade/positions/stream", h.streamTradePositions)
	api.POST("/trade/positions/:id/close", h.closeTradePosition)
	return r
}

func (h *Handler) health(c *gin.Context) {
	response.OK(c, gin.H{"status": "ok"})
}

func (h *Handler) searchTokens(c *gin.Context) {
	keyword := c.Query("keyword")
	items, err := h.backtestService.SearchTokens(c.Request.Context(), keyword, 20)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) getKlines(c *gin.Context) {
	start, err := parseOptionalTime(c.Query("startTime"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "startTime 格式错误")
		return
	}
	end, err := parseOptionalTime(c.Query("endTime"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "endTime 格式错误")
		return
	}
	items, err := h.backtestService.GetKlines(c.Request.Context(), c.Query("source"), datasource.KlineQuery{TokenAddress: c.Query("tokenAddress"), Interval: c.Query("interval"), StartTime: start, EndTime: end})
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

type createBacktestRequest struct {
	DataSource   string             `json:"dataSource"`
	TokenAddress string             `json:"tokenAddress" binding:"required"`
	TokenSymbol  string             `json:"tokenSymbol"`
	Interval     string             `json:"interval" binding:"required"`
	StartTime    time.Time          `json:"startTime" binding:"required"`
	EndTime      time.Time          `json:"endTime" binding:"required"`
	TradePoints  []model.TradePoint `json:"tradePoints" binding:"required"`
}

type strategyBacktestRunRequest struct {
	DataSource   string           `json:"dataSource"`
	MethodCode   string           `json:"methodCode" binding:"required"`
	MethodConfig json.RawMessage  `json:"methodConfig"`
	TokenAddress string           `json:"tokenAddress" binding:"required"`
	Interval     string           `json:"interval" binding:"required"`
	StartTime    time.Time        `json:"startTime" binding:"required"`
	EndTime      time.Time        `json:"endTime" binding:"required"`
	LevelOptions levelOptionsBody `json:"levelOptions"`
}

type levelOptionsBody struct {
	PivotWindow      int     `json:"pivotWindow"`
	PriceTolerance   float64 `json:"priceTolerance"`
	BreakTolerance   float64 `json:"breakTolerance"`
	ConfirmBars      int     `json:"confirmBars"`
	VolumeWindow     int     `json:"volumeWindow"`
	VolumeMultiplier float64 `json:"volumeMultiplier"`
	MaxLevels        int     `json:"maxLevels"`
	WindowSize       int     `json:"windowSize"`
	LevelWindowSize  int     `json:"levelWindowSize"`
	LevelWindowStep  int     `json:"levelWindowStep"`
	MinTouches       int     `json:"minTouches"`
	EntryOffsetBars  int     `json:"entryOffsetBars"`
	MaxHoldBars      int     `json:"maxHoldBars"`
	TakeProfitRR     float64 `json:"takeProfitRR"`
}

type realtimeSignalRequest struct {
	DataSource   string           `json:"dataSource"`
	TokenAddress string           `json:"tokenAddress" binding:"required"`
	Interval     string           `json:"interval" binding:"required"`
	StartTime    time.Time        `json:"startTime" binding:"required"`
	EndTime      time.Time        `json:"endTime" binding:"required"`
	LevelOptions levelOptionsBody `json:"levelOptions"`
	CurrentKline *model.Kline     `json:"currentKline,omitempty"`
}

type createBirdeyeAPIKeyRequest struct {
	APIKey string `json:"apiKey" binding:"required"`
}

type addCandidateMonitorRequest struct {
	TokenAddress string `json:"tokenAddress" binding:"required"`
}

func (h *Handler) getBirdeyeKlines(c *gin.Context) {
	c.Request.URL.RawQuery = c.Request.URL.Query().Encode()
	query := c.Request.URL.Query()
	query.Set("source", "birdeye")
	c.Request.URL.RawQuery = query.Encode()
	h.getKlines(c)
}

func (h *Handler) getGMGNKlines(c *gin.Context) {
	c.Request.URL.RawQuery = c.Request.URL.Query().Encode()
	query := c.Request.URL.Query()
	query.Set("source", "gmgn")
	c.Request.URL.RawQuery = query.Encode()
	h.getKlines(c)
}

func (h *Handler) getSupportResistance(c *gin.Context) {
	h.getSupportResistanceFromSource(c, c.Query("source"))
}

func (h *Handler) getBirdeyeSupportResistance(c *gin.Context) {
	h.getSupportResistanceFromSource(c, "birdeye")
}

func (h *Handler) getGMGNSupportResistance(c *gin.Context) {
	h.getSupportResistanceFromSource(c, "gmgn")
}

func (h *Handler) getSupportResistanceFromSource(c *gin.Context, source string) {
	start, err := parseTime(c.Query("startTime"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "startTime 格式错误")
		return
	}
	end, err := parseTime(c.Query("endTime"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "endTime 格式错误")
		return
	}
	options, ok := h.parseLevelOptions(c)
	if !ok {
		return
	}
	result, err := h.signalService.GetKlineLevelsFromSource(c.Request.Context(), source, datasource.KlineQuery{TokenAddress: c.Query("tokenAddress"), Interval: c.Query("interval"), StartTime: start, EndTime: end}, options)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) getRealtimeSignals(c *gin.Context) {
	h.getRealtimeSignalsFromSource(c, "")
}

func (h *Handler) getBirdeyeRealtimeSignals(c *gin.Context) {
	h.getRealtimeSignalsFromSource(c, "birdeye")
}

func (h *Handler) getGMGNRealtimeSignals(c *gin.Context) {
	h.getRealtimeSignalsFromSource(c, "gmgn")
}

func (h *Handler) getRealtimeSignalsFromSource(c *gin.Context, source string) {
	var req realtimeSignalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if source == "" {
		source = req.DataSource
	}
	options := backtest.DefaultLevelOptions()
	applyLevelOptionsBody(&options, req.LevelOptions)
	result, err := h.signalService.DetectRealtimeSignalsFromSource(c.Request.Context(), source, signal.RealtimeRequest{
		TokenAddress: req.TokenAddress,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		LevelOptions: options,
		CurrentKline: req.CurrentKline,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) createBirdeyeAPIKey(c *gin.Context) {
	var req createBirdeyeAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		response.Fail(c, http.StatusBadRequest, "Birdeye API Key 不能为空")
		return
	}
	if h.birdeyeKeyStore == nil {
		response.Fail(c, http.StatusBadRequest, "Birdeye API Key 池未启用")
		return
	}
	item, err := h.birdeyeKeyStore.AddKey(c.Request.Context(), apiKey)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) listCandidateMonitor(c *gin.Context) {
	if h.candidateMonitor == nil {
		response.OK(c, gin.H{"items": []signal.CandidateMonitorItem{}})
		return
	}
	items, err := h.candidateMonitor.ListCandidates(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) addCandidateMonitor(c *gin.Context) {
	var req addCandidateMonitorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "CA 不能为空")
		return
	}
	tokenAddress := strings.TrimSpace(req.TokenAddress)
	if tokenAddress == "" {
		response.Fail(c, http.StatusBadRequest, "CA 不能为空")
		return
	}
	if h.candidateMonitor == nil {
		response.Fail(c, http.StatusBadRequest, "候选池监控未启用")
		return
	}
	item, err := h.candidateMonitor.AddManualCandidate(c.Request.Context(), tokenAddress)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"item": item})
}

func (h *Handler) getDBSupportResistance(c *gin.Context) {
	var start time.Time
	var end time.Time
	if c.Query("range") != "all" {
		parsedStart, err := parseTime(c.Query("startTime"))
		if err != nil {
			response.Fail(c, http.StatusBadRequest, "startTime 格式错误")
			return
		}
		parsedEnd, err := parseTime(c.Query("endTime"))
		if err != nil {
			response.Fail(c, http.StatusBadRequest, "endTime 格式错误")
			return
		}
		start = parsedStart
		end = parsedEnd
	}
	pairID := c.Query("pairId")
	if pairID == "" {
		pairID = c.Query("tokenAddress")
	}
	options, ok := h.parseLevelOptions(c)
	if !ok {
		return
	}
	levels, err := h.backtestService.GetSupportResistance(c.Request.Context(), "db", datasource.KlineQuery{TokenAddress: pairID, Interval: c.Query("interval"), StartTime: start, EndTime: end}, options)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"levels": levels})
}

func (h *Handler) listStrategyMethods(c *gin.Context) {
	response.OK(c, gin.H{"items": h.backtestService.StrategyMethods()})
}

func (h *Handler) runStrategyBacktest(c *gin.Context) {
	var req strategyBacktestRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	options := backtest.DefaultLevelOptions()
	applyLevelOptionsBody(&options, req.LevelOptions)
	result, err := h.backtestService.RunStrategyBacktest(c.Request.Context(), req.DataSource, backtest.StrategyBacktestRequest{
		MethodCode:   req.MethodCode,
		MethodConfig: req.MethodConfig,
		TokenAddress: req.TokenAddress,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		LevelOptions: options,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) parseLevelOptions(c *gin.Context) (backtest.LevelOptions, bool) {
	options := backtest.DefaultLevelOptions()
	var ok bool
	if options.PivotWindow, ok = parseOptionalInt(c, "pivotWindow", options.PivotWindow); !ok {
		return options, false
	}
	if options.ConfirmBars, ok = parseOptionalInt(c, "confirmBars", options.ConfirmBars); !ok {
		return options, false
	}
	if options.VolumeWindow, ok = parseOptionalInt(c, "volumeWindow", options.VolumeWindow); !ok {
		return options, false
	}
	if options.MaxLevels, ok = parseOptionalInt(c, "maxLevels", options.MaxLevels); !ok {
		return options, false
	}
	if options.WindowSize, ok = parseOptionalInt(c, "windowSize", options.WindowSize); !ok {
		return options, false
	}
	if options.LevelWindowSize, ok = parseOptionalInt(c, "levelWindowSize", options.LevelWindowSize); !ok {
		return options, false
	}
	if options.LevelWindowStep, ok = parseOptionalInt(c, "levelWindowStep", options.LevelWindowStep); !ok {
		return options, false
	}
	if options.MinTouches, ok = parseOptionalInt(c, "minTouches", options.MinTouches); !ok {
		return options, false
	}
	if options.EntryOffsetBars, ok = parseOptionalNonNegativeInt(c, "entryOffsetBars", options.EntryOffsetBars); !ok {
		return options, false
	}
	if options.MaxHoldBars, ok = parseOptionalInt(c, "maxHoldBars", options.MaxHoldBars); !ok {
		return options, false
	}
	if options.PriceTolerance, ok = parseOptionalFloat(c, "priceTolerance", options.PriceTolerance); !ok {
		return options, false
	}
	if options.BreakTolerance, ok = parseOptionalFloat(c, "breakTolerance", options.BreakTolerance); !ok {
		return options, false
	}
	if options.VolumeMultiplier, ok = parseOptionalFloat(c, "volumeMultiplier", options.VolumeMultiplier); !ok {
		return options, false
	}
	if options.TakeProfitRR, ok = parseOptionalFloat(c, "takeProfitRR", options.TakeProfitRR); !ok {
		return options, false
	}
	return options, true
}

func parseOptionalInt(c *gin.Context, key string, fallback int) (int, bool) {
	value := c.Query(key)
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		response.Fail(c, http.StatusBadRequest, key+" 格式错误")
		return fallback, false
	}
	return parsed, true
}

func parseOptionalNonNegativeInt(c *gin.Context, key string, fallback int) (int, bool) {
	value := c.Query(key)
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		response.Fail(c, http.StatusBadRequest, key+" 格式错误")
		return fallback, false
	}
	return parsed, true
}

func parseOptionalFloat(c *gin.Context, key string, fallback float64) (float64, bool) {
	value := c.Query(key)
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		response.Fail(c, http.StatusBadRequest, key+" 格式错误")
		return fallback, false
	}
	return parsed, true
}

func applyLevelOptionsBody(options *backtest.LevelOptions, body levelOptionsBody) {
	if body.PivotWindow > 0 {
		options.PivotWindow = body.PivotWindow
	}
	if body.PriceTolerance > 0 {
		options.PriceTolerance = body.PriceTolerance
	}
	if body.BreakTolerance > 0 {
		options.BreakTolerance = body.BreakTolerance
	}
	if body.ConfirmBars > 0 {
		options.ConfirmBars = body.ConfirmBars
	}
	if body.VolumeWindow > 0 {
		options.VolumeWindow = body.VolumeWindow
	}
	if body.VolumeMultiplier > 0 {
		options.VolumeMultiplier = body.VolumeMultiplier
	}
	if body.MaxLevels > 0 {
		options.MaxLevels = body.MaxLevels
	}
	if body.WindowSize > 0 {
		options.WindowSize = body.WindowSize
	}
	if body.LevelWindowSize > 0 {
		options.LevelWindowSize = body.LevelWindowSize
	}
	if body.LevelWindowStep > 0 {
		options.LevelWindowStep = body.LevelWindowStep
	}
	if body.MinTouches > 0 {
		options.MinTouches = body.MinTouches
	}
	if body.EntryOffsetBars >= 0 {
		options.EntryOffsetBars = body.EntryOffsetBars
	}
	if body.MaxHoldBars > 0 {
		options.MaxHoldBars = body.MaxHoldBars
	}
	if body.TakeProfitRR > 0 {
		options.TakeProfitRR = body.TakeProfitRR
	}
}

func (h *Handler) createBacktest(c *gin.Context) {
	var req createBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	result, err := h.backtestService.AnalyzeAndSave(c.Request.Context(), backtest.AnalyzeRequest{SessionID: uuid.NewString(), DataSource: req.DataSource, TokenAddress: req.TokenAddress, TokenSymbol: req.TokenSymbol, Interval: req.Interval, StartTime: req.StartTime, EndTime: req.EndTime, TradePoints: req.TradePoints})
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) listBacktests(c *gin.Context) {
	items, err := h.backtestService.ListAnalyses(c.Request.Context(), 50)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) getBacktest(c *gin.Context) {
	item, err := h.backtestService.GetAnalysis(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) listTradeAccounts(c *gin.Context) {
	items, err := h.tradeService.ListAccounts(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) getTradeRuntime(c *gin.Context) {
	response.OK(c, gin.H{
		"tradeMode": h.tradeService.GetTradeMode(),
		"options": []gin.H{
			{"label": "模拟盘", "value": model.TradeModePaper},
			{"label": "实盘", "value": model.TradeModeLive},
		},
	})
}

type updateTradeRuntimeRequest struct {
	TradeMode model.TradeMode `json:"tradeMode" binding:"required"`
}

func (h *Handler) updateTradeRuntime(c *gin.Context) {
	var req updateTradeRuntimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	mode, err := h.tradeService.UpdateTradeMode(c.Request.Context(), req.TradeMode)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"tradeMode": mode})
}

func (h *Handler) listTradeSignals(c *gin.Context) {
	mode, ok := parseTradeModeFilter(c)
	if !ok {
		return
	}
	items, err := h.tradeService.ListSignals(c.Request.Context(), mode, parseLimit(c.Query("limit"), 100))
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) listTradeSummary(c *gin.Context) {
	items, err := h.tradeService.ListTradeSummaries(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) listTradeOrders(c *gin.Context) {
	mode, ok := parseTradeModeFilter(c)
	if !ok {
		return
	}
	items, err := h.tradeService.ListOrders(c.Request.Context(), mode, parseLimit(c.Query("limit"), 100))
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) getTradeOrder(c *gin.Context) {
	item, err := h.tradeService.GetOrder(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) retryTradeOrder(c *gin.Context) {
	item, err := h.tradeService.RetryOrder(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) listTradePositions(c *gin.Context) {
	mode, ok := parseTradeModeFilter(c)
	if !ok {
		return
	}
	items, err := h.tradeService.ListPositions(c.Request.Context(), c.Query("status"), mode, parseLimit(c.Query("limit"), 100))
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) closeTradePosition(c *gin.Context) {
	item, err := h.tradeService.ClosePosition(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, datasource.ErrQueryNotConfigured):
		response.Fail(c, http.StatusBadRequest, "数据源查询 SQL 未配置")
	case errors.Is(err, datasource.ErrBirdeyeNotConfigured):
		response.Fail(c, http.StatusBadRequest, "Birdeye API Key 未配置")
	case errors.Is(err, datasource.ErrBirdeyeNoAvailableKey):
		response.Fail(c, http.StatusBadRequest, "Birdeye 可用 API Key 不存在")
	case errors.Is(err, datasource.ErrGMGNNotConfigured):
		response.Fail(c, http.StatusBadRequest, "GMGN API Key 未配置")
	case errors.Is(err, datasource.ErrUnsupportedKlineSource):
		response.Fail(c, http.StatusBadRequest, "K 线数据源仅支持 gmgn/birdeye/sql/db")
	case errors.Is(err, datasource.ErrUnsupportedPriceSource):
		response.Fail(c, http.StatusBadRequest, "价格数据源仅支持 gmgn/dexscreener")
	case errors.Is(err, datasource.ErrBitqueryNotConfigured):
		response.Fail(c, http.StatusBadRequest, "Bitquery API Token 未配置")
	case errors.Is(err, backtest.ErrStrategyMethodNotFound):
		response.Fail(c, http.StatusBadRequest, "回测方法不存在")
	case errors.Is(err, backtest.ErrInvalidTimeRange):
		response.Fail(c, http.StatusBadRequest, "开始时间必须早于结束时间")
	case errors.Is(err, backtest.ErrNoKlines):
		response.Fail(c, http.StatusBadRequest, "指定时间范围内没有 K 线数据")
	case errors.Is(err, backtest.ErrInvalidTradeFlow):
		response.Fail(c, http.StatusBadRequest, "买卖点必须按买入后卖出的顺序成对出现")
	case errors.Is(err, trade.ErrTradeDisabled):
		response.Fail(c, http.StatusBadRequest, "交易模块未启用")
	case errors.Is(err, trade.ErrTradeExecutionNotReady):
		response.Fail(c, http.StatusBadRequest, "Jupiter 执行器尚未配置完成")
	case errors.Is(err, trade.ErrInvalidTradeMode):
		response.Fail(c, http.StatusBadRequest, err.Error())
	default:
		response.Fail(c, http.StatusInternalServerError, err.Error())
	}
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("时间不能为空")
	}
	return parseOptionalTime(value)
}

func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return apptime.InBeijing(parsed), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04:05", value, apptime.Beijing)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func parseLimit(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseTradeModeFilter(c *gin.Context) (model.TradeMode, bool) {
	mode, ok := parseTradeModeValue(c.Query("tradeMode"))
	if !ok {
		responseBadTradeMode(c)
		return "", false
	}
	return mode, true
}
