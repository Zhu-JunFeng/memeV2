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

type BirdeyeTradePointDataSource struct {
	client   *http.Client
	baseURL  string
	apiKeys  []string
	chain    string
	maxPages int
	cursor   uint32
}

type birdeyeTokenTxResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []birdeyeTokenTx `json:"items"`
	} `json:"data"`
	Message string `json:"message"`
}

type birdeyeTokenTx struct {
	Owner         string  `json:"owner"`
	Side          string  `json:"side"`
	BlockUnixTime int64   `json:"blockUnixTime"`
	TokenPrice    float64 `json:"tokenPrice"`
	TxHash        string  `json:"txHash"`
}

func NewBirdeyeTradePointDataSource(baseURL string, apiKeys []string, chain string, maxPages int) *BirdeyeTradePointDataSource {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://public-api.birdeye.so"
	}
	if strings.TrimSpace(chain) == "" {
		chain = "solana"
	}
	if maxPages <= 0 {
		maxPages = 1
	}
	return &BirdeyeTradePointDataSource{
		client:   &http.Client{Timeout: 20 * time.Second},
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKeys:  normalizeKeys(apiKeys),
		chain:    strings.TrimSpace(chain),
		maxPages: maxPages,
	}
}

func (s *BirdeyeTradePointDataSource) GetTradePoints(ctx context.Context, req TradePointQuery) ([]model.TradePoint, error) {
	if len(s.apiKeys) == 0 {
		return nil, ErrBirdeyeNotConfigured
	}
	if strings.TrimSpace(req.WalletAddress) == "" {
		return nil, errors.New("钱包地址不能为空")
	}
	const limit = 50
	points := make([]model.TradePoint, 0)
	maxPages := s.maxPages
	if req.MaxPages > 0 {
		maxPages = req.MaxPages
	}
	for page := 0; page < maxPages; page++ {
		items, err := s.fetchTokenTxPage(ctx, req.TokenAddress, page*limit, limit)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		shouldStop := false
		for _, item := range items {
			pointTime := apptime.InBeijing(time.Unix(item.BlockUnixTime, 0))
			if pointTime.Before(req.StartTime) {
				shouldStop = true
				continue
			}
			if pointTime.After(req.EndTime) || item.Owner != req.WalletAddress {
				continue
			}
			side := model.TradeSide(item.Side)
			if side != model.TradeSideBuy && side != model.TradeSideSell {
				continue
			}
			price := item.TokenPrice
			points = append(points, model.TradePoint{Side: side, Time: pointTime, Price: &price, Note: item.TxHash})
		}
		if shouldStop {
			break
		}
	}
	return points, nil
}

func (s *BirdeyeTradePointDataSource) fetchTokenTxPage(ctx context.Context, tokenAddress string, offset int, limit int) ([]birdeyeTokenTx, error) {
	endpoint, err := url.Parse(s.baseURL + "/defi/txs/token")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("address", tokenAddress)
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()

	var lastErr error
	for attempt := 0; attempt < len(s.apiKeys); attempt++ {
		key := nextBirdeyeKey(s.apiKeys, &s.cursor)
		body, err := s.doTokenTxRequest(ctx, endpoint.String(), key)
		if err == nil {
			return body.Data.Items, nil
		}
		lastErr = err
		if !shouldRetryBirdeye(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (s *BirdeyeTradePointDataSource) doTokenTxRequest(ctx context.Context, endpoint string, apiKey string) (*birdeyeTokenTxResponse, error) {
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
	var body birdeyeTokenTxResponse
	if err := decodeBirdeyeBody(resp, &body); err != nil {
		if shouldRetryBirdeye(err) {
			return nil, err
		}
		if apiErr, ok := err.(*birdeyeAPIError); ok && apiErr.statusCode > 0 {
			return nil, fmt.Errorf("Birdeye 交易接口返回状态码 %d", apiErr.statusCode)
		}
		return nil, err
	}
	if !body.Success {
		err := birdeyeBodyError(body.Message, "Birdeye 交易接口返回失败")
		if shouldRetryBirdeye(err) {
			return nil, err
		}
		return nil, err
	}
	return &body, nil
}
