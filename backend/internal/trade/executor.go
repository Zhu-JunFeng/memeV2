package trade

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	bin "github.com/gagliardetto/binary"
	solana "github.com/gagliardetto/solana-go"
	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/httpclient"
	"solana-meme-backtest/backend/internal/model"
)

const wrappedSOLMint = "So11111111111111111111111111111111111111112"
const lamportsPerSOL = 1_000_000_000

type JupiterExecutor struct {
	cfg           config.TradeConfig
	client        *http.Client
	priceProvider datasource.TokenPriceProvider
	privateKey    solana.PrivateKey
	walletAddress string
}

type jupiterOrderResponse struct {
	Transaction               string `json:"transaction"`
	RequestID                 string `json:"requestId"`
	InputMint                 string `json:"inputMint"`
	OutputMint                string `json:"outputMint"`
	InAmount                  string `json:"inAmount"`
	OutAmount                 string `json:"outAmount"`
	InUsdValue                any    `json:"inUsdValue"`
	OutUsdValue               any    `json:"outUsdValue"`
	ErrorCode                 any    `json:"errorCode"`
	ErrorMessage              string `json:"errorMessage"`
	PrioritizationFeeLamports int64  `json:"prioritizationFeeLamports"`
	SignatureFeeLamports      int64  `json:"signatureFeeLamports"`
	RentFeeLamports           int64  `json:"rentFeeLamports"`
}

type jupiterQuoteResponse struct {
	InputMint            string `json:"inputMint"`
	InAmount             string `json:"inAmount"`
	OutputMint           string `json:"outputMint"`
	OutAmount            string `json:"outAmount"`
	OtherAmountThreshold string `json:"otherAmountThreshold"`
	SwapMode             string `json:"swapMode"`
	SlippageBps          int    `json:"slippageBps"`
	PriceImpactPct       string `json:"priceImpactPct"`
}

type jupiterExecuteRequest struct {
	SignedTransaction string `json:"signedTransaction"`
	RequestID         string `json:"requestId"`
}

type jupiterExecuteResponse struct {
	Status             string `json:"status"`
	Signature          string `json:"signature"`
	RequestID          string `json:"requestId"`
	InputAmountResult  string `json:"inputAmountResult"`
	OutputAmountResult string `json:"outputAmountResult"`
	Error              string `json:"error"`
	Code               any    `json:"code"`
}

type rpcTokenSupplyResponse struct {
	Result struct {
		Value struct {
			Decimals uint8 `json:"decimals"`
		} `json:"value"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewJupiterExecutor(cfg config.TradeConfig, priceProvider datasource.TokenPriceProvider) (*JupiterExecutor, error) {
	privateKeyText := strings.TrimSpace(cfg.WalletPrivateKey)
	if privateKeyText == "" {
		return nil, fmt.Errorf("交易钱包私钥未配置")
	}
	apiKey := strings.TrimSpace(cfg.Jupiter.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("Jupiter API Key 未配置")
	}
	privateKey, err := solana.PrivateKeyFromBase58(privateKeyText)
	if err != nil {
		return nil, fmt.Errorf("交易钱包私钥格式错误: %w", err)
	}
	walletAddress := privateKey.PublicKey().String()
	if configured := strings.TrimSpace(cfg.WalletAddress); configured != "" && configured != walletAddress {
		return nil, fmt.Errorf("配置的钱包地址与私钥推导地址不一致: %s != %s", configured, walletAddress)
	}
	return &JupiterExecutor{
		cfg:           cfg,
		priceProvider: priceProvider,
		privateKey:    privateKey,
		walletAddress: walletAddress,
		client:        httpclient.NewFixedProxyClient(30*time.Second, 15*time.Second),
	}, nil
}

// Execute 按“下订单 -> 本地签名 -> 提交执行”三步走，
// 这样交易事实只以 Jupiter 的真实成交结果为准，不复用计划值充当 fill。
func (e *JupiterExecutor) Execute(ctx context.Context, req ExecutionRequest) (ExecutionResult, error) {
	if req.Mode == model.TradeModePaper {
		quoteResp, err := e.getQuote(ctx, req)
		if err != nil {
			return ExecutionResult{}, err
		}
		return e.buildPaperExecutionResult(ctx, req, quoteResp)
	}
	orderResp, err := e.getOrder(ctx, req)
	if err != nil {
		return ExecutionResult{}, err
	}
	if strings.TrimSpace(orderResp.Transaction) == "" {
		return ExecutionResult{}, fmt.Errorf("Jupiter 下单未返回交易数据: %s", defaultString(orderResp.ErrorMessage, "unknown error"))
	}
	signedTransaction, err := e.signTransaction(orderResp.Transaction)
	if err != nil {
		return ExecutionResult{}, err
	}
	execResp, err := e.executeOrder(ctx, signedTransaction, orderResp.RequestID)
	if err != nil {
		return ExecutionResult{}, err
	}
	if !strings.EqualFold(execResp.Status, "success") {
		return ExecutionResult{}, fmt.Errorf("Jupiter 执行失败: %s", defaultString(execResp.Error, execResp.Status))
	}
	result, err := e.buildExecutionResult(ctx, req, orderResp, execResp)
	if err != nil {
		return ExecutionResult{}, err
	}
	return result, nil
}

func (e *JupiterExecutor) Quote(ctx context.Context, req ExecutionRequest) (QuoteResult, error) {
	quoteResp, err := e.getQuote(ctx, req)
	if err != nil {
		return QuoteResult{}, err
	}
	return e.buildQuoteResult(ctx, req, quoteResp)
}

func (e *JupiterExecutor) buildPaperExecutionResult(ctx context.Context, req ExecutionRequest, quoteResp jupiterQuoteResponse) (ExecutionResult, error) {
	solPriceUSD, err := e.priceProvider.GetTokenPrice(ctx, wrappedSOLMint)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("获取 SOL 美元价格失败: %w", err)
	}
	requestPayload, err := json.Marshal(map[string]any{
		"walletAddress": e.walletAddress,
		"side":          req.Order.Side,
		"mode":          req.Mode,
		"simulatedBy":   "jupiter_quote",
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	responsePayload, err := json.Marshal(map[string]any{
		"quote": quoteResp,
		"paper": map[string]any{
			"simulated": true,
			"reason":    "paper mode uses Jupiter quote only",
		},
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	result := ExecutionResult{
		RequestPayload:   requestPayload,
		ResponsePayload:  responsePayload,
		TxHash:           "paper_" + uuid.NewString(),
		FeeAmount:        0,
		FeeAsset:         "USD",
		ExecutedAt:       time.Now().UTC(),
		Simulated:        true,
		ExecutionChannel: string(model.TradeExecutionChannelJupiterPaper),
	}
	switch req.Order.Side {
	case model.TradeSignalTypeBuy:
		decimals, err := e.fetchMintDecimals(ctx, req.Signal.TokenAddress)
		if err != nil {
			return ExecutionResult{}, err
		}
		filledToken, err := rawAmountToDecimal(quoteResp.OutAmount, decimals)
		if err != nil {
			return ExecutionResult{}, err
		}
		spentSOL, err := rawAmountToDecimal(quoteResp.InAmount, 9)
		if err != nil {
			return ExecutionResult{}, err
		}
		result.FilledToken = filledToken
		result.FilledQuote = spentSOL * solPriceUSD
		if result.FilledToken > 0 {
			result.AvgPrice = result.FilledQuote / result.FilledToken
		}
	case model.TradeSignalTypeSell:
		decimals, err := e.fetchMintDecimals(ctx, req.Signal.TokenAddress)
		if err != nil {
			return ExecutionResult{}, err
		}
		soldToken, err := rawAmountToDecimal(quoteResp.InAmount, decimals)
		if err != nil {
			return ExecutionResult{}, err
		}
		receivedSOL, err := rawAmountToDecimal(quoteResp.OutAmount, 9)
		if err != nil {
			return ExecutionResult{}, err
		}
		result.FilledToken = soldToken
		result.FilledQuote = receivedSOL * solPriceUSD
		if result.FilledToken > 0 {
			result.AvgPrice = result.FilledQuote / result.FilledToken
		}
	default:
		return ExecutionResult{}, fmt.Errorf("不支持的交易方向: %s", req.Order.Side)
	}
	return result, nil
}

func (e *JupiterExecutor) buildQuoteResult(ctx context.Context, req ExecutionRequest, quoteResp jupiterQuoteResponse) (QuoteResult, error) {
	solPriceUSD, err := e.priceProvider.GetTokenPrice(ctx, wrappedSOLMint)
	if err != nil {
		return QuoteResult{}, fmt.Errorf("获取 SOL 美元价格失败: %w", err)
	}
	switch req.Order.Side {
	case model.TradeSignalTypeBuy:
		decimals, err := e.fetchMintDecimals(ctx, req.Signal.TokenAddress)
		if err != nil {
			return QuoteResult{}, err
		}
		filledToken, err := rawAmountToDecimal(quoteResp.OutAmount, decimals)
		if err != nil {
			return QuoteResult{}, err
		}
		spentSOL, err := rawAmountToDecimal(quoteResp.InAmount, 9)
		if err != nil {
			return QuoteResult{}, err
		}
		filledQuote := spentSOL * solPriceUSD
		result := QuoteResult{FilledToken: filledToken, FilledQuote: filledQuote}
		if filledToken > 0 {
			result.AvgPrice = filledQuote / filledToken
		}
		return result, nil
	case model.TradeSignalTypeSell:
		decimals, err := e.fetchMintDecimals(ctx, req.Signal.TokenAddress)
		if err != nil {
			return QuoteResult{}, err
		}
		soldToken, err := rawAmountToDecimal(quoteResp.InAmount, decimals)
		if err != nil {
			return QuoteResult{}, err
		}
		receivedSOL, err := rawAmountToDecimal(quoteResp.OutAmount, 9)
		if err != nil {
			return QuoteResult{}, err
		}
		filledQuote := receivedSOL * solPriceUSD
		result := QuoteResult{FilledToken: soldToken, FilledQuote: filledQuote}
		if soldToken > 0 {
			result.AvgPrice = filledQuote / soldToken
		}
		return result, nil
	default:
		return QuoteResult{}, fmt.Errorf("不支持的交易方向: %s", req.Order.Side)
	}
}

func (e *JupiterExecutor) getQuote(ctx context.Context, req ExecutionRequest) (jupiterQuoteResponse, error) {
	amount, inputMint, outputMint, err := e.resolveOrderAmount(ctx, req)
	if err != nil {
		return jupiterQuoteResponse{}, err
	}
	endpoint := strings.TrimRight(defaultString(e.cfg.Jupiter.BaseURL, "https://lite-api.jup.ag"), "/") + "/swap/v1/quote"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return jupiterQuoteResponse{}, err
	}
	query := httpReq.URL.Query()
	query.Set("inputMint", inputMint)
	query.Set("outputMint", outputMint)
	query.Set("amount", amount)
	query.Set("slippageBps", strconv.Itoa(maxInt(req.Config.SlippageBPS, 1)))
	httpReq.URL.RawQuery = query.Encode()
	httpReq.Header.Set("accept", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return jupiterQuoteResponse{}, err
	}
	defer resp.Body.Close()
	var body jupiterQuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return jupiterQuoteResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jupiterQuoteResponse{}, fmt.Errorf("Jupiter 报价返回状态码 %d", resp.StatusCode)
	}
	if strings.TrimSpace(body.InAmount) == "" || strings.TrimSpace(body.OutAmount) == "" {
		return jupiterQuoteResponse{}, fmt.Errorf("Jupiter 模拟报价失败: 未返回有效数量")
	}
	return body, nil
}

func (e *JupiterExecutor) getOrder(ctx context.Context, req ExecutionRequest) (jupiterOrderResponse, error) {
	amount, inputMint, outputMint, err := e.resolveOrderAmount(ctx, req)
	if err != nil {
		return jupiterOrderResponse{}, err
	}
	endpoint := strings.TrimRight(defaultString(e.cfg.Jupiter.BaseURL, "https://lite-api.jup.ag"), "/") + "/ultra/v1/order"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return jupiterOrderResponse{}, err
	}
	query := httpReq.URL.Query()
	query.Set("inputMint", inputMint)
	query.Set("outputMint", outputMint)
	query.Set("amount", amount)
	query.Set("taker", e.walletAddress)
	query.Set("slippageBps", strconv.Itoa(maxInt(req.Config.SlippageBPS, 1)))
	httpReq.URL.RawQuery = query.Encode()
	httpReq.Header.Set("x-api-key", e.cfg.Jupiter.APIKey)
	httpReq.Header.Set("accept", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return jupiterOrderResponse{}, err
	}
	defer resp.Body.Close()
	var body jupiterOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return jupiterOrderResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jupiterOrderResponse{}, fmt.Errorf("Jupiter 下单返回状态码 %d: %s", resp.StatusCode, localizeJupiterMessage(defaultString(body.ErrorMessage, "request failed")))
	}
	if body.ErrorMessage != "" {
		return jupiterOrderResponse{}, fmt.Errorf("Jupiter 下单失败: %s", localizeJupiterMessage(body.ErrorMessage))
	}
	return body, nil
}

func (e *JupiterExecutor) executeOrder(ctx context.Context, signedTransaction string, requestID string) (jupiterExecuteResponse, error) {
	endpoint := strings.TrimRight(defaultString(e.cfg.Jupiter.BaseURL, "https://lite-api.jup.ag"), "/") + "/ultra/v1/execute"
	payload, err := json.Marshal(jupiterExecuteRequest{SignedTransaction: signedTransaction, RequestID: requestID})
	if err != nil {
		return jupiterExecuteResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return jupiterExecuteResponse{}, err
	}
	httpReq.Header.Set("x-api-key", e.cfg.Jupiter.APIKey)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return jupiterExecuteResponse{}, err
	}
	defer resp.Body.Close()
	var body jupiterExecuteResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return jupiterExecuteResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jupiterExecuteResponse{}, fmt.Errorf("Jupiter 执行返回状态码 %d: %s", resp.StatusCode, localizeJupiterMessage(defaultString(body.Error, "request failed")))
	}
	return body, nil
}

func (e *JupiterExecutor) signTransaction(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("解码 Jupiter 交易失败: %w", err)
	}
	tx, err := solana.TransactionFromDecoder(bin.NewBinDecoder(raw))
	if err != nil {
		return "", fmt.Errorf("解析 Jupiter 交易失败: %w", err)
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(e.privateKey.PublicKey()) {
			return &e.privateKey
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("签名 Jupiter 交易失败: %w", err)
	}
	signed, err := tx.ToBase64()
	if err != nil {
		return "", fmt.Errorf("编码签名交易失败: %w", err)
	}
	return signed, nil
}

func (e *JupiterExecutor) resolveOrderAmount(ctx context.Context, req ExecutionRequest) (amount string, inputMint string, outputMint string, err error) {
	switch req.Order.Side {
	case model.TradeSignalTypeBuy:
		lamports := uint64(math.Round(req.Account.BuyAmountSOL * lamportsPerSOL))
		if lamports == 0 {
			return "", "", "", fmt.Errorf("买入 SOL 数量过小: %.8f", req.Account.BuyAmountSOL)
		}
		return strconv.FormatUint(lamports, 10), wrappedSOLMint, req.Signal.TokenAddress, nil
	case model.TradeSignalTypeSell:
		decimals, err := e.fetchMintDecimals(ctx, req.Signal.TokenAddress)
		if err != nil {
			return "", "", "", err
		}
		rawAmount, err := decimalAmountToRaw(req.Position.Quantity, decimals)
		if err != nil {
			return "", "", "", err
		}
		return rawAmount.String(), req.Signal.TokenAddress, wrappedSOLMint, nil
	default:
		return "", "", "", fmt.Errorf("不支持的交易方向: %s", req.Order.Side)
	}
}

func (e *JupiterExecutor) buildExecutionResult(ctx context.Context, req ExecutionRequest, orderResp jupiterOrderResponse, execResp jupiterExecuteResponse) (ExecutionResult, error) {
	solPriceUSD, err := e.priceProvider.GetTokenPrice(ctx, wrappedSOLMint)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("获取 SOL 美元价格失败: %w", err)
	}
	feeLamports := maxInt64(orderResp.PrioritizationFeeLamports, 0) + maxInt64(orderResp.SignatureFeeLamports, 0) + maxInt64(orderResp.RentFeeLamports, 0)
	feeUSD := float64(feeLamports) / lamportsPerSOL * solPriceUSD
	responsePayload, err := json.Marshal(map[string]any{
		"order":   orderResp,
		"execute": execResp,
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	requestPayload, err := json.Marshal(map[string]any{
		"walletAddress": e.walletAddress,
		"requestId":     orderResp.RequestID,
		"side":          req.Order.Side,
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	result := ExecutionResult{
		RequestPayload:   requestPayload,
		ResponsePayload:  responsePayload,
		TxHash:           execResp.Signature,
		FeeAmount:        feeUSD,
		FeeAsset:         "USD",
		ExecutedAt:       time.Now().UTC(),
		ExecutionChannel: string(model.TradeExecutionChannelJupiterLive),
	}
	switch req.Order.Side {
	case model.TradeSignalTypeBuy:
		decimals, err := e.fetchMintDecimals(ctx, req.Signal.TokenAddress)
		if err != nil {
			return ExecutionResult{}, err
		}
		filledToken, err := rawAmountToDecimal(execResp.OutputAmountResult, decimals)
		if err != nil {
			return ExecutionResult{}, err
		}
		spentSOL, err := rawAmountToDecimal(execResp.InputAmountResult, 9)
		if err != nil {
			return ExecutionResult{}, err
		}
		filledQuoteUSD := spentSOL * solPriceUSD
		result.FilledToken = filledToken
		result.FilledQuote = filledQuoteUSD
		if filledToken > 0 {
			result.AvgPrice = filledQuoteUSD / filledToken
		}
	case model.TradeSignalTypeSell:
		decimals, err := e.fetchMintDecimals(ctx, req.Signal.TokenAddress)
		if err != nil {
			return ExecutionResult{}, err
		}
		soldToken, err := rawAmountToDecimal(execResp.InputAmountResult, decimals)
		if err != nil {
			return ExecutionResult{}, err
		}
		receivedSOL, err := rawAmountToDecimal(execResp.OutputAmountResult, 9)
		if err != nil {
			return ExecutionResult{}, err
		}
		filledQuoteUSD := receivedSOL * solPriceUSD
		result.FilledToken = soldToken
		result.FilledQuote = filledQuoteUSD
		if soldToken > 0 {
			result.AvgPrice = filledQuoteUSD / soldToken
		}
	}
	return result, nil
}

func (e *JupiterExecutor) feeUSD(solPriceUSD float64, orderResp jupiterOrderResponse) float64 {
	feeLamports := maxInt64(orderResp.PrioritizationFeeLamports, 0) + maxInt64(orderResp.SignatureFeeLamports, 0) + maxInt64(orderResp.RentFeeLamports, 0)
	return float64(feeLamports) / lamportsPerSOL * solPriceUSD
}

func (e *JupiterExecutor) fetchMintDecimals(ctx context.Context, mint string) (uint8, error) {
	if mint == wrappedSOLMint {
		return 9, nil
	}
	endpoint := defaultString(e.cfg.SolanaRPCURL, "https://api.mainnet-beta.solana.com")
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getTokenSupply",
		"params":  []any{mint, map[string]any{"commitment": "confirmed"}},
	})
	if err != nil {
		return 0, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var body rpcTokenSupplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("Solana RPC 返回状态码 %d", resp.StatusCode)
	}
	if body.Error != nil {
		return 0, fmt.Errorf("Solana RPC 获取 decimals 失败: %s", body.Error.Message)
	}
	return body.Result.Value.Decimals, nil
}

func rawAmountToDecimal(value string, decimals uint8) (float64, error) {
	parsed, ok := new(big.Int).SetString(strings.TrimSpace(value), 10)
	if !ok {
		return 0, fmt.Errorf("解析原始数量失败: %s", value)
	}
	ratio := new(big.Rat).SetInt(parsed)
	divisor := new(big.Rat).SetFloat64(math.Pow10(int(decimals)))
	result, _ := new(big.Rat).Quo(ratio, divisor).Float64()
	return result, nil
}

func decimalAmountToRaw(value float64, decimals uint8) (*big.Int, error) {
	if value <= 0 {
		return nil, fmt.Errorf("卖出数量无效: %f", value)
	}
	scaled := value * math.Pow10(int(decimals))
	if scaled <= 0 {
		return nil, fmt.Errorf("卖出数量折算后无效: %f", value)
	}
	return big.NewInt(int64(math.Round(scaled))), nil
}

func maxInt(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func maxInt64(value int64, fallback int64) int64 {
	if value < fallback {
		return fallback
	}
	return value
}

func localizeJupiterMessage(message string) string {
	normalized := strings.TrimSpace(message)
	if normalized == "" {
		return "请求失败"
	}
	lower := strings.ToLower(normalized)
	switch {
	case strings.Contains(lower, "insufficient funds"):
		return "余额不足"
	case strings.Contains(lower, "slippage"):
		return "滑点超出限制"
	case strings.Contains(lower, "route not found"):
		return "未找到可执行的交易路径"
	default:
		return normalized
	}
}
