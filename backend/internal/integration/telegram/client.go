package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

const apiBaseURL = "https://api.telegram.org"

type Client struct {
	httpClient *http.Client
	baseURL    string
	botToken   string
	chatID     string
}

func NewClient(botToken, chatID string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    apiBaseURL,
		botToken:   strings.TrimSpace(botToken),
		chatID:     strings.TrimSpace(chatID),
	}
}

func (c *Client) NotifySignal(ctx context.Context, signal model.TradeSignal) error {
	text := fmt.Sprintf(
		"%s %s信号\n模式：%s\nCA：%s\n策略：%s\n触发市值：%s\n原因：%s\n时间：%s",
		directionIcon(signal.SignalType), directionText(signal.SignalType), modeText(signal.TradeMode), signal.TokenAddress,
		signal.StrategyCode, formatMarketCap(signal.TriggerMarketCap), signal.Reason, signal.SignalTime.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"),
	)
	if signal.SignalType == model.TradeSignalTypeSell {
		if profitRate, ok := signalProfitRate(signal.RawPayloadJSON); ok {
			text += "\n盈亏（U）：" + formatPercent(profitRate)
		}
	}
	return c.sendMessage(ctx, text)
}

func (c *Client) NotifyTrade(ctx context.Context, fill model.TradeFill) error {
	text := fmt.Sprintf(
		"%s %s交易成功\n模式：%s\nCA：%s",
		directionIcon(fill.Side), directionText(fill.Side), modeText(fill.TradeMode), fill.TokenAddress,
	)
	if fill.ExecutedMarketCap > 0 {
		text += "\n成交市值：" + formatMarketCap(fill.ExecutedMarketCap)
	}
	text += fmt.Sprintf("\n交易哈希：%s\n时间：%s", fill.TxHash, fill.ExecutedAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"))
	if fill.Side == model.TradeSignalTypeSell {
		text += "\n盈亏（U）：" + formatPercent(fill.ProfitRate)
	}
	return c.sendMessage(ctx, text)
}

func (c *Client) NotifyTradeModeChange(ctx context.Context, change model.TradeModeChange) error {
	text := fmt.Sprintf(
		"🔄 交易模式已切换\n原模式：%s\n新模式：%s\n钱包：%s\n时间：%s",
		modeText(change.PreviousMode), modeText(change.CurrentMode), change.WalletAddress,
		change.ChangedAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"),
	)
	return c.sendMessage(ctx, text)
}

func signalProfitRate(raw json.RawMessage) (float64, bool) {
	var payload struct {
		Metadata struct {
			ProfitRate *float64 `json:"profitRate"`
		} `json:"metadata"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || payload.Metadata.ProfitRate == nil {
		return 0, false
	}
	return *payload.Metadata.ProfitRate, true
}

func formatMarketCap(value float64) string {
	return fmt.Sprintf("%.2fk", value/1000)
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%+.2f%%", value*100)
}

func (c *Client) sendMessage(ctx context.Context, text string) error {
	payload, err := json.Marshal(map[string]string{"chat_id": c.chatID, "text": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.botToken+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram sendMessage HTTP %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 200))
	}
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("解析 Telegram 响应失败: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("Telegram sendMessage 失败: %s", result.Description)
	}
	return nil
}

func modeText(mode model.TradeMode) string {
	if mode == model.TradeModeLive {
		return "实盘"
	}
	return "模拟盘"
}

func directionText(side model.TradeSignalType) string {
	if side == model.TradeSignalTypeSell {
		return "卖出"
	}
	return "买入"
}

func directionIcon(side model.TradeSignalType) string {
	if side == model.TradeSignalTypeSell {
		return "🔴"
	}
	return "🟢"
}

func formatNumber(value float64) string {
	formatted := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.9f", value), "0"), ".")
	if formatted == "" || formatted == "-0" {
		return "0"
	}
	return formatted
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
