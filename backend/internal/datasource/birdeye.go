package datasource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/model"
)

var ErrBirdeyeNotConfigured = errors.New("Birdeye API Key 未配置")
var ErrBirdeyeNoAvailableKey = errors.New("Birdeye 可用 API Key 不存在")

type BirdeyeDataSource struct {
	client  *http.Client
	baseURL string
	apiKeys []string
	keyPool BirdeyeKeyPool
	chain   string
	cursor  uint32
}

type birdeyeOHLCVResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []birdeyeOHLCVItem `json:"items"`
	} `json:"data"`
	Message string `json:"message"`
}

type birdeyeOHLCVItem struct {
	UnixTime int64   `json:"unix_time"`
	Address  string  `json:"address"`
	Type     string  `json:"type"`
	Open     float64 `json:"o"`
	High     float64 `json:"h"`
	Low      float64 `json:"l"`
	Close    float64 `json:"c"`
	Volume   float64 `json:"v"`
}

type birdeyeMarketDataResponse struct {
	Success bool `json:"success"`
	Data    struct {
		CirculatingSupply float64 `json:"circulating_supply"`
		MarketCap         float64 `json:"market_cap"`
	} `json:"data"`
	Message string `json:"message"`
}

func NewBirdeyeDataSource(baseURL string, apiKeys []string, chain string) *BirdeyeDataSource {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://public-api.birdeye.so"
	}
	if strings.TrimSpace(chain) == "" {
		chain = "solana"
	}
	return &BirdeyeDataSource{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKeys: normalizeKeys(apiKeys),
		chain:   strings.TrimSpace(chain),
	}
}

func (s *BirdeyeDataSource) WithKeyPool(keyPool BirdeyeKeyPool) *BirdeyeDataSource {
	s.keyPool = keyPool
	return s
}

func (s *BirdeyeDataSource) GetKlines(ctx context.Context, req KlineQuery) ([]model.Kline, error) {
	if _, err := s.availableKeys(ctx); err != nil {
		return nil, err
	}
	circulatingSupply, err := s.fetchCirculatingSupply(ctx, req.TokenAddress)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(s.baseURL + "/defi/v3/ohlcv")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("address", req.TokenAddress)
	query.Set("type", birdeyeInterval(req.Interval))
	query.Set("time_from", strconv.FormatInt(req.StartTime.Unix(), 10))
	query.Set("time_to", strconv.FormatInt(req.EndTime.Unix(), 10))
	endpoint.RawQuery = query.Encode()
	body, err := s.fetchOHLCV(ctx, endpoint.String())
	if err != nil {
		return nil, err
	}

	items := make([]model.Kline, 0, len(body.Data.Items))
	for _, item := range body.Data.Items {
		openTime := apptime.InBeijing(time.Unix(item.UnixTime, 0))
		items = append(items, model.Kline{
			TokenAddress:   req.TokenAddress,
			Interval:       req.Interval,
			OpenTime:       openTime,
			CloseTime:      birdeyeCloseTime(openTime, req.Interval),
			MarketCapOpen:  item.Open * circulatingSupply,
			MarketCapHigh:  item.High * circulatingSupply,
			MarketCapLow:   item.Low * circulatingSupply,
			MarketCapClose: item.Close * circulatingSupply,
			// Birdeye 原始 volume 是 token 成交数量；回测量能统一按成交额口径使用。
			Volume: item.Volume * item.Close,
		})
	}
	return items, nil
}

func (s *BirdeyeDataSource) fetchCirculatingSupply(ctx context.Context, tokenAddress string) (float64, error) {
	endpoint, err := url.Parse(s.baseURL + "/defi/v3/token/market-data")
	if err != nil {
		return 0, err
	}
	query := endpoint.Query()
	query.Set("address", tokenAddress)
	endpoint.RawQuery = query.Encode()
	body, err := s.fetchMarketData(ctx, endpoint.String())
	if err != nil {
		return 0, err
	}
	if body.Data.CirculatingSupply <= 0 {
		return 0, errors.New("Birdeye 未返回有效流通供应量，无法按市值计算")
	}
	return body.Data.CirculatingSupply, nil
}

func (s *BirdeyeDataSource) fetchOHLCV(ctx context.Context, endpoint string) (*birdeyeOHLCVResponse, error) {
	keys, err := s.availableKeys(ctx)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < len(keys); attempt++ {
		key := nextBirdeyeKey(keys, &s.cursor)
		body, err := s.doOHLCVRequest(ctx, endpoint, key)
		if err == nil {
			s.markSuccessful(ctx, key)
			return body, nil
		}
		lastErr = err
		s.markUnavailableIfNeeded(ctx, key, err)
	}
	return nil, lastErr
}

func (s *BirdeyeDataSource) fetchMarketData(ctx context.Context, endpoint string) (*birdeyeMarketDataResponse, error) {
	keys, err := s.availableKeys(ctx)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < len(keys); attempt++ {
		key := nextBirdeyeKey(keys, &s.cursor)
		body, err := s.doMarketDataRequest(ctx, endpoint, key)
		if err == nil {
			s.markSuccessful(ctx, key)
			return body, nil
		}
		lastErr = err
		s.markUnavailableIfNeeded(ctx, key, err)
	}
	return nil, lastErr
}

func (s *BirdeyeDataSource) availableKeys(ctx context.Context) ([]string, error) {
	if s.keyPool == nil {
		if len(s.apiKeys) == 0 {
			return nil, ErrBirdeyeNotConfigured
		}
		return s.apiKeys, nil
	}
	keys, err := s.keyPool.ListAvailableBirdeyeKeys(ctx)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, ErrBirdeyeNoAvailableKey
	}
	return keys, nil
}

func (s *BirdeyeDataSource) markUnavailableIfNeeded(ctx context.Context, apiKey string, err error) {
	if s.keyPool == nil || !isBirdeyeComputeUnitLimit(err) {
		return
	}
	_ = s.keyPool.MarkBirdeyeKeyUnavailable(ctx, apiKey, err.Error())
}

func (s *BirdeyeDataSource) markSuccessful(ctx context.Context, apiKey string) {
	if s.keyPool == nil {
		return
	}
	_ = s.keyPool.MarkBirdeyeKeySuccessful(ctx, apiKey)
}

func (s *BirdeyeDataSource) doOHLCVRequest(ctx context.Context, endpoint string, apiKey string) (*birdeyeOHLCVResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-API-KEY", apiKey)
	httpReq.Header.Set("x-chain", s.chain)
	httpReq.Header.Set("accept", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body birdeyeOHLCVResponse
	if err := decodeBirdeyeBody(resp, &body); err != nil {
		if shouldRetryBirdeye(err) {
			return nil, err
		}
		if apiErr, ok := err.(*birdeyeAPIError); ok && apiErr.statusCode > 0 {
			return nil, fmt.Errorf("Birdeye K线接口返回状态码 %d", apiErr.statusCode)
		}
		return nil, err
	}
	if !body.Success {
		err := birdeyeBodyError(body.Message, "Birdeye K线接口返回失败")
		if shouldRetryBirdeye(err) {
			return nil, err
		}
		return nil, err
	}
	return &body, nil
}

func (s *BirdeyeDataSource) doMarketDataRequest(ctx context.Context, endpoint string, apiKey string) (*birdeyeMarketDataResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-API-KEY", apiKey)
	httpReq.Header.Set("x-chain", s.chain)
	httpReq.Header.Set("accept", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body birdeyeMarketDataResponse
	if err := decodeBirdeyeBody(resp, &body); err != nil {
		if shouldRetryBirdeye(err) {
			return nil, err
		}
		if apiErr, ok := err.(*birdeyeAPIError); ok && apiErr.statusCode > 0 {
			return nil, fmt.Errorf("Birdeye 市值接口返回状态码 %d", apiErr.statusCode)
		}
		return nil, err
	}
	if !body.Success {
		err := birdeyeBodyError(body.Message, "Birdeye 市值接口返回失败")
		if shouldRetryBirdeye(err) {
			return nil, err
		}
		return nil, err
	}
	return &body, nil
}

func normalizeKeys(keys []string) []string {
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		normalized = append(normalized, key)
	}
	return normalized
}

func birdeyeInterval(interval string) string {
	switch strings.TrimSpace(interval) {
	case "1h":
		return "1H"
	case "2h":
		return "2H"
	case "4h":
		return "4H"
	case "1d":
		return "1D"
	case "1w":
		return "1W"
	default:
		return interval
	}
}

func birdeyeCloseTime(openTime time.Time, interval string) time.Time {
	switch strings.TrimSpace(interval) {
	case "1m":
		return openTime.Add(time.Minute)
	case "3m":
		return openTime.Add(3 * time.Minute)
	case "5m":
		return openTime.Add(5 * time.Minute)
	case "15m":
		return openTime.Add(15 * time.Minute)
	case "30m":
		return openTime.Add(30 * time.Minute)
	case "1h":
		return openTime.Add(time.Hour)
	case "2h":
		return openTime.Add(2 * time.Hour)
	case "4h":
		return openTime.Add(4 * time.Hour)
	case "1d":
		return openTime.AddDate(0, 0, 1)
	case "1w":
		return openTime.AddDate(0, 0, 7)
	default:
		return openTime
	}
}
