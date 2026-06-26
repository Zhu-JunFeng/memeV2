package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type TokenPriceProvider interface {
	GetTokenPrice(ctx context.Context, tokenAddress string) (float64, error)
}

type DexScreenerPriceSource struct {
	client  *http.Client
	baseURL string
}

type dexScreenerResponse struct {
	Pairs []struct {
		PriceUSD string `json:"priceUsd"`
	} `json:"pairs"`
}

func NewDexScreenerPriceSource(baseURL string) *DexScreenerPriceSource {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = "https://api.dexscreener.com"
	}
	return &DexScreenerPriceSource{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: strings.TrimRight(trimmed, "/"),
	}
}

func (s *DexScreenerPriceSource) GetTokenPrice(ctx context.Context, tokenAddress string) (float64, error) {
	endpoint := fmt.Sprintf("%s/latest/dex/tokens/%s", s.baseURL, tokenAddress)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("DexScreener 返回状态码 %d", resp.StatusCode)
	}
	var body dexScreenerResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	for _, pair := range body.Pairs {
		value := strings.TrimSpace(pair.PriceUSD)
		if value == "" {
			continue
		}
		price, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		if price > 0 {
			return price, nil
		}
	}
	return 0, fmt.Errorf("DexScreener 未返回 %s 的有效价格", tokenAddress)
}
