package datasource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/model"
)

var ErrBitqueryNotConfigured = errors.New("Bitquery API Token 未配置")

type BitqueryTradePointDataSource struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

type bitqueryGraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type bitqueryGraphQLResponse struct {
	Data   bitquerySolanaData `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type bitquerySolanaData struct {
	Solana struct {
		DEXTradeByTokens []bitqueryDexTradeByToken `json:"DEXTradeByTokens"`
	} `json:"Solana"`
}

type bitqueryDexTradeByToken struct {
	Block struct {
		Time time.Time `json:"Time"`
	} `json:"Block"`
	Transaction struct {
		Signature string `json:"Signature"`
		Signer    string `json:"Signer"`
	} `json:"Transaction"`
	Trade struct {
		Price      float64 `json:"Price"`
		PriceInUSD float64 `json:"PriceInUSD"`
		Side       struct {
			Type string `json:"Type"`
		} `json:"Side"`
		Account struct {
			Owner string `json:"Owner"`
		} `json:"Account"`
	} `json:"Trade"`
}

func NewBitqueryTradePointDataSource(baseURL, apiKey string) *BitqueryTradePointDataSource {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://streaming.bitquery.io/graphql"
	}
	return &BitqueryTradePointDataSource{
		client:  &http.Client{Timeout: 25 * time.Second},
		baseURL: strings.TrimSpace(baseURL),
		apiKey:  strings.TrimSpace(apiKey),
	}
}

func (s *BitqueryTradePointDataSource) GetTradePoints(ctx context.Context, req TradePointQuery) ([]model.TradePoint, error) {
	if s.apiKey == "" {
		return nil, ErrBitqueryNotConfigured
	}
	if strings.TrimSpace(req.WalletAddress) == "" {
		return nil, errors.New("钱包地址不能为空")
	}
	limit := 100
	if req.MaxPages > 0 {
		limit = req.MaxPages * 100
	}
	payload := bitqueryGraphQLRequest{
		Query: bitqueryWalletTokenTradesQuery,
		Variables: map[string]interface{}{
			"token":  req.TokenAddress,
			"wallet": req.WalletAddress,
			"since":  req.StartTime.UTC().Format(time.RFC3339),
			"till":   req.EndTime.UTC().Format(time.RFC3339),
			"limit":  limit,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Bitquery 交易接口返回状态码 %d", resp.StatusCode)
	}
	var result bitqueryGraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, errors.New(result.Errors[0].Message)
	}
	points := make([]model.TradePoint, 0, len(result.Data.Solana.DEXTradeByTokens))
	seen := make(map[string]struct{})
	for _, item := range result.Data.Solana.DEXTradeByTokens {
		side := model.TradeSide(strings.ToLower(item.Trade.Side.Type))
		if side != model.TradeSideBuy && side != model.TradeSideSell {
			continue
		}
		key := item.Transaction.Signature + "|" + string(side) + "|" + item.Block.Time.UTC().Format(time.RFC3339)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		price := item.Trade.PriceInUSD
		if price == 0 {
			price = item.Trade.Price
		}
		points = append(points, model.TradePoint{Side: side, Time: apptime.InBeijing(item.Block.Time), Price: &price, Note: item.Transaction.Signature})
	}
	return points, nil
}

const bitqueryWalletTokenTradesQuery = `query WalletTokenTrades($token: String!, $wallet: String!, $since: DateTime!, $till: DateTime!, $limit: Int!) {
  Solana(dataset: realtime) {
    DEXTradeByTokens(
      orderBy: {ascending: Block_Time}
      limit: {count: $limit}
      where: {
        Block: {Time: {since: $since, till: $till}}
        Transaction: {Result: {Success: true}}
        Trade: {
          Currency: {MintAddress: {is: $token}}
          Account: {Owner: {is: $wallet}}
        }
      }
    ) {
      Block { Time }
      Transaction { Signature Signer }
      Trade {
        Price
        PriceInUSD
        Account { Owner }
        Side { Type }
      }
    }
  }
}`
