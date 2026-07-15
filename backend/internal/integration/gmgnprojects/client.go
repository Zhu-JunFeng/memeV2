package gmgnprojects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/httpclient"
	"solana-meme-backtest/backend/internal/integration/gmgnkeys"
	"solana-meme-backtest/backend/internal/integration/project"
)

const rateLimitCooldown = time.Minute

type Client struct {
	httpClient *http.Client
	baseURL    string
	keys       *gmgnkeys.Scheduler
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://openapi.gmgn.ai"
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		keys:       gmgnkeys.NewScheduler(nil, []string{apiKey}, 0),
	}
}

func (c *Client) WithKeyScheduler(scheduler *gmgnkeys.Scheduler) *Client {
	c.keys = scheduler
	return c
}

func (c *Client) WithHTTPObserver(observer httpclient.HTTPObserver) *Client {
	c.httpClient = httpclient.WithObserver(c.httpClient, "GMGN 项目源", observer)
	return c
}

func (c *Client) FetchCompletedProjects(ctx context.Context) ([]project.Item, error) {
	keys, err := c.keys.AvailableKeys(ctx)
	if err != nil {
		return nil, err
	}
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
	type trenchesResponse struct {
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
	var response trenchesResponse
	var lastErr error
	for attempt := 0; attempt < len(keys); attempt++ {
		key := c.keys.NextKey(keys)
		if err := c.keys.Wait(ctx, key); err != nil {
			if errors.Is(err, gmgnkeys.ErrKeyCoolingDown) {
				lastErr = err
				continue
			}
			return nil, err
		}
		raw, statusCode, err := c.do(ctx, http.MethodPost, u.String(), payload, key)
		if err != nil {
			return nil, err
		}
		if statusCode == http.StatusTooManyRequests {
			c.keys.MarkRateLimited(key, rateLimitCooldown)
			lastErr = fmt.Errorf("GMGN trenches 触发限流: status=%d body=%s", statusCode, truncate(strings.TrimSpace(string(raw)), 200))
			continue
		}
		response = trenchesResponse{}
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, fmt.Errorf("解析 GMGN trenches 失败: %w", err)
		}
		if response.Code == http.StatusTooManyRequests {
			c.keys.MarkRateLimited(key, rateLimitCooldown)
			lastErr = fmt.Errorf("GMGN trenches 触发限流: status=%d code=%d message=%s", statusCode, response.Code, response.Message)
			continue
		}
		if statusCode < 200 || statusCode >= 300 {
			return nil, fmt.Errorf("GMGN trenches HTTP %d: %s", statusCode, truncate(strings.TrimSpace(string(raw)), 200))
		}
		if response.Code != 0 {
			return nil, fmt.Errorf("GMGN trenches 返回异常: code=%d message=%s", response.Code, response.Message)
		}
		c.keys.MarkSuccessful(ctx, key)
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, lastErr
	}
	items := make([]project.Item, 0, len(response.Data.Completed))
	for _, item := range response.Data.Completed {
		items = append(items, project.Item{TokenAddress: strings.TrimSpace(item.Address), Symbol: strings.TrimSpace(item.Symbol), KOL: item.RenownedCount, MarketCap: item.MarketCap})
	}
	return items, nil
}

func (c *Client) FetchTokenSymbol(ctx context.Context, tokenAddress string) (string, error) {
	keys, err := c.keys.AvailableKeys(ctx)
	if err != nil {
		return "", err
	}
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
	type tokenInfoResponse struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Symbol string `json:"symbol"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	var response tokenInfoResponse
	var lastErr error
	for attempt := 0; attempt < len(keys); attempt++ {
		key := c.keys.NextKey(keys)
		if err := c.keys.Wait(ctx, key); err != nil {
			if errors.Is(err, gmgnkeys.ErrKeyCoolingDown) {
				lastErr = err
				continue
			}
			return "", err
		}
		raw, statusCode, err := c.do(ctx, http.MethodGet, u.String(), nil, key)
		if err != nil {
			return "", err
		}
		if statusCode == http.StatusTooManyRequests {
			c.keys.MarkRateLimited(key, rateLimitCooldown)
			lastErr = fmt.Errorf("GMGN token info 触发限流: status=%d body=%s", statusCode, truncate(strings.TrimSpace(string(raw)), 200))
			continue
		}
		response = tokenInfoResponse{}
		if err := json.Unmarshal(raw, &response); err != nil {
			return "", err
		}
		if response.Code == http.StatusTooManyRequests {
			c.keys.MarkRateLimited(key, rateLimitCooldown)
			lastErr = fmt.Errorf("GMGN token info 触发限流: status=%d code=%d message=%s", statusCode, response.Code, response.Message)
			continue
		}
		if statusCode < 200 || statusCode >= 300 || response.Code != 0 {
			return "", fmt.Errorf("GMGN token info 返回异常: status=%d code=%d message=%s", statusCode, response.Code, response.Message)
		}
		c.keys.MarkSuccessful(ctx, key)
		lastErr = nil
		break
	}
	if lastErr != nil {
		return "", lastErr
	}
	symbol := strings.TrimSpace(response.Data.Symbol)
	if symbol == "" {
		symbol = strings.TrimSpace(response.Data.Name)
	}
	return symbol, nil
}

func (c *Client) do(ctx context.Context, method, endpoint string, payload []byte, apiKey string) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-APIKEY", apiKey)
	req.Header.Set("User-Agent", "solana-meme-backtest-v2/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
