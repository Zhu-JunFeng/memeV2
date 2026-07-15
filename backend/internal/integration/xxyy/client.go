package xxyy

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

	"solana-meme-backtest/backend/internal/httpclient"
	"solana-meme-backtest/backend/internal/integration/project"
)

const BaseURL = "https://www.xxyy.io"

type FeedItem = project.Item

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = BaseURL
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey != "" && !strings.HasPrefix(strings.ToLower(apiKey), "bearer ") {
		apiKey = "Bearer " + apiKey
	}
	return &Client{httpClient: httpClient, baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey}
}

func (c *Client) WithHTTPObserver(observer httpclient.HTTPObserver) *Client {
	c.httpClient = httpclient.WithObserver(c.httpClient, "XXYY 项目源", observer)
	return c
}

func (c *Client) FetchCompletedProjects(ctx context.Context) ([]project.Item, error) {
	endpoint := c.baseURL + "/api/trade/open/api/feed/" + url.PathEscape("COMPLETED") + "?chain=sol"
	body := map[string]any{
		"dex":         []string{"pump", "pumpmayhem", "bonk", "launchlab", "mdbcbags"},
		"mc":          "15000,",
		"liq":         "2000,",
		"vol":         "3000,",
		"holder":      "50,",
		"createTime":  "2,2400",
		"devHp":       ",35",
		"topHp":       ",35",
		"insiderHp":   ",35",
		"bundleHp":    ",35",
		"newWalletHp": ",35",
		"snipers":     ",35",
		"kol":         "3,",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)
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
		return nil, fmt.Errorf("XXYY feed HTTP %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 200))
	}
	var response struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("解析 XXYY feed 失败: %w", err)
	}
	items := make([]project.Item, 0, len(response.Data.Items))
	for _, item := range response.Data.Items {
		address := firstString(item["ca"], item["tokenAddress"])
		items = append(items, project.Item{
			TokenAddress: address,
			Symbol:       firstString(item["symbol"], item["name"]),
			KOL:          firstFloat(item["kolNum"], item["kol"]),
			MarketCap:    firstFloat(item["marketCapUSD"], item["marketCap"]),
		})
	}
	return items, nil
}

func firstString(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func firstFloat(values ...any) float64 {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return typed
		case json.Number:
			result, _ := typed.Float64()
			return result
		case string:
			var result float64
			if _, err := fmt.Sscan(strings.TrimSpace(typed), &result); err == nil {
				return result
			}
		}
	}
	return 0
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
