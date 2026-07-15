package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/httpclient"
	"solana-meme-backtest/backend/internal/integration/gmgnkeys"
	"solana-meme-backtest/backend/internal/model"
)

var ErrGMGNNotConfigured = errors.New("GMGN API Key 未配置")

const defaultGMGNUserAgent = "solana-meme-backtest-v2/1.0"
const gmgnRateLimitCooldown = time.Minute

type GMGNDataSource struct {
	client       *http.Client
	baseURL      string
	keyScheduler *gmgnkeys.Scheduler
	maxQPS       float64
	chain        string
	userAgent    string
	lastWindow   time.Duration
}

type gmgnKlineResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
	Error   string `json:"error"`
	ResetAt int64  `json:"reset_at"`
	Data    struct {
		List []gmgnKlineItem `json:"list"`
	} `json:"data"`
}

type gmgnKlineItem struct {
	Time   int64  `json:"time"`
	Open   string `json:"open"`
	Close  string `json:"close"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Volume string `json:"volume"`
	Amount string `json:"amount"`
	Source string `json:"source"`
}

func NewGMGNDataSource(baseURL string, apiKey string, chain string, maxQPS float64) *GMGNDataSource {
	return NewGMGNDataSourceWithKeys(baseURL, []string{apiKey}, chain, maxQPS)
}

func NewGMGNDataSourceWithKeys(baseURL string, apiKeys []string, chain string, maxQPS float64) *GMGNDataSource {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = "https://openapi.gmgn.ai"
	}
	chain = strings.TrimSpace(chain)
	if chain == "" {
		chain = "sol"
	}
	return &GMGNDataSource{
		client:       httpclient.NewDirectClient(15*time.Second, 15*time.Second),
		baseURL:      strings.TrimRight(trimmed, "/"),
		keyScheduler: gmgnkeys.NewScheduler(nil, apiKeys, maxQPS),
		maxQPS:       maxQPS,
		chain:        chain,
		userAgent:    defaultGMGNUserAgent,
		lastWindow:   3 * time.Minute,
	}
}

func (s *GMGNDataSource) WithKeyPool(keyPool GMGNKeyPool) *GMGNDataSource {
	s.keyScheduler = gmgnkeys.NewScheduler(keyPool, nil, s.maxQPS)
	return s
}

func (s *GMGNDataSource) WithKeyScheduler(scheduler *gmgnkeys.Scheduler) *GMGNDataSource {
	s.keyScheduler = scheduler
	return s
}

func (s *GMGNDataSource) WithHTTPObserver(observer httpclient.HTTPObserver) *GMGNDataSource {
	s.client = httpclient.WithObserver(s.client, "GMGN K线/报价", observer)
	return s
}

func (s *GMGNDataSource) GetKlines(ctx context.Context, req KlineQuery) ([]model.Kline, error) {
	keys, err := s.keyScheduler.AvailableKeys(ctx)
	if err != nil {
		if errors.Is(err, gmgnkeys.ErrNoAvailableKey) {
			return nil, ErrGMGNNotConfigured
		}
		return nil, err
	}
	endpoint, err := url.Parse(s.baseURL + "/v1/market/token_kline")
	if err != nil {
		return nil, err
	}
	start, end := normalizeGMGNRange(req)
	query := endpoint.Query()
	query.Set("chain", s.chain)
	query.Set("address", req.TokenAddress)
	query.Set("resolution", gmgnInterval(req.Interval))
	query.Set("from", strconv.FormatInt(start.UnixMilli(), 10))
	query.Set("to", strconv.FormatInt(end.UnixMilli(), 10))
	query.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	query.Set("client_id", uuid.NewString())
	endpoint.RawQuery = query.Encode()

	var lastErr error
	for attempt := 0; attempt < len(keys); attempt++ {
		key := s.keyScheduler.NextKey(keys)
		if err := s.keyScheduler.Wait(ctx, key); err != nil {
			if errors.Is(err, gmgnkeys.ErrKeyCoolingDown) {
				lastErr = err
				continue
			}
			return nil, err
		}
		items, err := s.fetchKlinesWithKey(ctx, endpoint.String(), req, key)
		if err == nil {
			s.keyScheduler.MarkSuccessful(ctx, key)
			return items, nil
		}
		lastErr = err
		if !shouldRetryGMGN(err) {
			return nil, err
		}
		s.keyScheduler.MarkRateLimited(key, gmgnRateLimitCooldown)
	}
	return nil, lastErr
}

func (s *GMGNDataSource) fetchKlinesWithKey(ctx context.Context, endpoint string, req KlineQuery, apiKey string) ([]model.Kline, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-APIKEY", apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", s.userAgent)
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &gmgnAPIError{statusCode: resp.StatusCode, message: fmt.Sprintf("GMGN K线接口触发限流: %s", truncateGMGNBody(raw, 200))}
	}
	var body gmgnKlineResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("GMGN K线接口返回状态码 %d: %s", resp.StatusCode, truncateGMGNBody(raw, 200))
		}
		return nil, fmt.Errorf("解析 GMGN K线响应失败: %w", err)
	}
	if body.Code == http.StatusTooManyRequests {
		return nil, &gmgnAPIError{statusCode: resp.StatusCode, code: body.Code, message: fmt.Sprintf("GMGN K线接口触发限流: %s", gmgnErrorMessage(body))}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GMGN K线接口返回状态码 %d: %s", resp.StatusCode, gmgnErrorMessage(body))
	}
	if body.Code != 0 {
		return nil, fmt.Errorf("GMGN K线接口返回失败: %s", gmgnErrorMessage(body))
	}
	items := make([]model.Kline, 0, len(body.Data.List))
	for _, raw := range body.Data.List {
		item, err := gmgnItemToKline(req.TokenAddress, req.Interval, raw)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].OpenTime.Before(items[j].OpenTime) })
	return items, nil
}

func truncateGMGNBody(raw []byte, limit int) string {
	value := strings.TrimSpace(string(raw))
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (s *GMGNDataSource) GetTokenPrice(ctx context.Context, tokenAddress string) (float64, error) {
	now := time.Now().UTC()
	klines, err := s.GetKlines(ctx, KlineQuery{TokenAddress: tokenAddress, Interval: "1m", StartTime: now.Add(-s.lastWindow), EndTime: now})
	if err != nil {
		return 0, err
	}
	if len(klines) == 0 {
		return 0, fmt.Errorf("GMGN 未返回 %s 的有效价格", tokenAddress)
	}
	price := klines[len(klines)-1].Close
	if price <= 0 {
		return 0, fmt.Errorf("GMGN 未返回 %s 的有效价格", tokenAddress)
	}
	return price, nil
}

func gmgnItemToKline(tokenAddress string, interval string, raw gmgnKlineItem) (model.Kline, error) {
	open, err := parseGMGNFloat(raw.Open, "open")
	if err != nil {
		return model.Kline{}, err
	}
	high, err := parseGMGNFloat(raw.High, "high")
	if err != nil {
		return model.Kline{}, err
	}
	low, err := parseGMGNFloat(raw.Low, "low")
	if err != nil {
		return model.Kline{}, err
	}
	closeValue, err := parseGMGNFloat(raw.Close, "close")
	if err != nil {
		return model.Kline{}, err
	}
	volume, err := parseGMGNFloat(raw.Volume, "volume")
	if err != nil {
		return model.Kline{}, err
	}
	openTime := apptime.InBeijing(time.UnixMilli(raw.Time))
	return model.Kline{
		TokenAddress:   tokenAddress,
		Interval:       interval,
		OpenTime:       openTime,
		CloseTime:      closeTime(openTime, interval),
		Open:           open,
		High:           high,
		Low:            low,
		Close:          closeValue,
		MarketCapOpen:  open,
		MarketCapHigh:  high,
		MarketCapLow:   low,
		MarketCapClose: closeValue,
		Volume:         volume,
	}, nil
}

func parseGMGNFloat(value string, field string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("GMGN K线字段 %s 格式错误: %w", field, err)
	}
	return parsed, nil
}

func normalizeGMGNRange(req KlineQuery) (time.Time, time.Time) {
	end := req.EndTime
	if end.IsZero() {
		end = time.Now().UTC()
	}
	start := req.StartTime
	if start.IsZero() {
		start = end.Add(-3 * time.Minute)
	}
	return start, end
}

func gmgnInterval(interval string) string {
	return strings.TrimSpace(interval)
}

func gmgnErrorMessage(body gmgnKlineResponse) string {
	for _, value := range []string{body.Message, body.Reason, body.Error} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	if body.ResetAt > 0 {
		return fmt.Sprintf("reset_at=%d", body.ResetAt)
	}
	return "unknown error"
}
