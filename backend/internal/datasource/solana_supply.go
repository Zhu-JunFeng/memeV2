package datasource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"solana-meme-backtest/backend/internal/httpclient"
)

type TokenSupplyProvider interface {
	GetTokenSupply(ctx context.Context, mint string) (float64, error)
}

type SolanaRPCSupplyProvider struct {
	client *http.Client
	url    string
	mu     sync.Mutex
	cache  map[string]supplyCacheItem
	ttl    time.Duration
}

type supplyCacheItem struct {
	value     float64
	expiresAt time.Time
}

type rpcSupplyResponse struct {
	Result struct {
		Value struct {
			Amount         string  `json:"amount"`
			Decimals       uint8   `json:"decimals"`
			UIAmount       float64 `json:"uiAmount"`
			UIAmountString string  `json:"uiAmountString"`
		} `json:"value"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewSolanaRPCSupplyProvider(rpcURL string) *SolanaRPCSupplyProvider {
	trimmed := strings.TrimSpace(rpcURL)
	if trimmed == "" {
		trimmed = "https://api.mainnet-beta.solana.com"
	}
	return &SolanaRPCSupplyProvider{
		client: httpclient.NewFixedProxyClient(15*time.Second, 15*time.Second),
		url:    trimmed,
		cache:  map[string]supplyCacheItem{},
		ttl:    10 * time.Minute,
	}
}

func (p *SolanaRPCSupplyProvider) GetTokenSupply(ctx context.Context, mint string) (float64, error) {
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return 0, fmt.Errorf("Solana mint 不能为空")
	}
	now := time.Now()
	p.mu.Lock()
	cached, ok := p.cache[mint]
	if ok && now.Before(cached.expiresAt) {
		p.mu.Unlock()
		return cached.value, nil
	}
	p.mu.Unlock()

	supply, err := p.fetchSupply(ctx, mint)
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	p.cache[mint] = supplyCacheItem{value: supply, expiresAt: now.Add(p.ttl)}
	p.mu.Unlock()
	return supply, nil
}

func (p *SolanaRPCSupplyProvider) fetchSupply(ctx context.Context, mint string) (float64, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getTokenSupply",
		"params":  []any{mint, map[string]any{"commitment": "confirmed"}},
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var body rpcSupplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("Solana RPC 返回状态码 %d", resp.StatusCode)
	}
	if body.Error != nil {
		return 0, fmt.Errorf("Solana RPC 获取 token supply 失败: %s", body.Error.Message)
	}
	if body.Result.Value.UIAmountString != "" {
		value, err := strconv.ParseFloat(body.Result.Value.UIAmountString, 64)
		if err != nil {
			return 0, fmt.Errorf("Solana RPC supply 格式错误: %w", err)
		}
		return value, nil
	}
	if body.Result.Value.UIAmount > 0 {
		return body.Result.Value.UIAmount, nil
	}
	return 0, fmt.Errorf("Solana RPC 未返回有效 token supply")
}
