package gmgnprojects

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/integration/project"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://openapi.gmgn.ai"
	}
	return &Client{httpClient: httpClient, baseURL: strings.TrimRight(baseURL, "/"), apiKey: strings.TrimSpace(apiKey)}
}

func (c *Client) FetchCompletedProjects(ctx context.Context) ([]project.Item, error) {
	u, err := url.Parse(c.baseURL + "/v1/trenches")
	if err != nil {
		return nil, err
	}
	query := u.Query()
	query.Set("chain", "sol")
	query.Set("timestamp", fmt.Sprint(time.Now().Unix()))
	query.Set("client_id", uuid.NewString())
	u.RawQuery = query.Encode()
	body := map[string]any{
		"version": "v2",
		"completed": map[string]any{
			"filters":               []string{"offchain", "onchain"},
			"launchpad_platform_v2": true,
			"limit":                 80,
			"min_renowned_count":    3,
			"launchpad_platform": []string{
				"Pump.fun", "pump_mayhem", "pump_mayhem_agent", "pump_agent", "letsbonk", "bonkers", "bags", "memoo", "liquid", "bankr", "zora", "surge", "anoncoin", "moonshot_app", "wendotdev", "heaven", "sugar", "token_mill", "believe", "trendsfun", "trends_fun", "jup_studio", "Moonshot", "boop", "ray_launchpad", "meteora_virtual_curve", "xstocks",
			},
			"quote_address_type": []int{4, 5, 3, 1, 13, 0},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-APIKEY", c.apiKey)
	req.Header.Set("User-Agent", "solana-meme-backtest-v2/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GMGN trenches HTTP %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 200))
	}
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Completed []struct {
				Address       string  `json:"address"`
				Symbol        string  `json:"symbol"`
				RenownedCount float64 `json:"renowned_count"`
				MarketCap     float64 `json:"usd_market_cap"`
			} `json:"completed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("解析 GMGN trenches 失败: %w", err)
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("GMGN trenches 返回异常: code=%d message=%s", response.Code, response.Message)
	}
	items := make([]project.Item, 0, len(response.Data.Completed))
	for _, item := range response.Data.Completed {
		items = append(items, project.Item{TokenAddress: strings.TrimSpace(item.Address), Symbol: strings.TrimSpace(item.Symbol), KOL: item.RenownedCount, MarketCap: item.MarketCap})
	}
	return items, nil
}

func (c *Client) FetchTokenSymbol(ctx context.Context, tokenAddress string) (string, error) {
	u, err := url.Parse(c.baseURL + "/v1/token/info")
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("chain", "sol")
	query.Set("address", strings.TrimSpace(tokenAddress))
	query.Set("timestamp", fmt.Sprint(time.Now().Unix()))
	query.Set("client_id", uuid.NewString())
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-APIKEY", c.apiKey)
	req.Header.Set("User-Agent", "solana-meme-backtest-v2/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Symbol string `json:"symbol"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || response.Code != 0 {
		return "", fmt.Errorf("GMGN token info 返回异常: status=%d code=%d message=%s", resp.StatusCode, response.Code, response.Message)
	}
	symbol := strings.TrimSpace(response.Data.Symbol)
	if symbol == "" {
		symbol = strings.TrimSpace(response.Data.Name)
	}
	return symbol, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
