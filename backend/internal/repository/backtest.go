package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/model"
)

type BacktestRepository struct {
	db *sql.DB
}

func NewBacktestRepository(db *sql.DB) *BacktestRepository {
	return &BacktestRepository{db: db}
}

func (r *BacktestRepository) SaveAnalysis(ctx context.Context, input backtest.SaveAnalysisInput) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := apptime.InBeijing(time.Now())
	_, err = tx.ExecContext(ctx, `INSERT INTO backtest_sessions (id, token_address, token_symbol, `+"`interval`"+`, start_time, end_time, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, input.Session.ID, input.Session.TokenAddress, input.Session.TokenSymbol, input.Session.Interval, input.Session.StartTime, input.Session.EndTime, now, now)
	if err != nil {
		return err
	}
	pointIDs := make([]string, 0, len(input.Points))
	for _, point := range input.Points {
		id := uuid.NewString()
		pointIDs = append(pointIDs, id)
		_, err := tx.ExecContext(ctx, `INSERT INTO backtest_trade_points (id, session_id, side, point_time, input_price, note, matched_kline_time, matched_price, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, input.Session.ID, point.Side, point.Time, point.Price, point.Note, point.MatchedKlineTime, point.MatchedPrice, now)
		if err != nil {
			return err
		}
	}
	for i, trade := range input.Trades {
		buyID := ""
		sellID := ""
		if i*2+1 < len(pointIDs) {
			buyID = pointIDs[i*2]
			sellID = pointIDs[i*2+1]
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO backtest_trade_results (id, session_id, buy_point_id, sell_point_id, buy_matched_kline_time, sell_matched_kline_time, buy_price, sell_price, profit, profit_rate, holding_seconds, win, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), input.Session.ID, buyID, sellID, trade.Buy.MatchedKlineTime, trade.Sell.MatchedKlineTime, trade.Buy.MatchedPrice, trade.Sell.MatchedPrice, trade.Profit, trade.ProfitRate, trade.HoldingSeconds, trade.Win, now)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO backtest_metric_snapshots (id, session_id, trade_count, win_rate, total_profit_rate, max_drawdown_rate, average_holding_seconds, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), input.Session.ID, input.Metrics.TradeCount, input.Metrics.WinRate, input.Metrics.TotalProfitRate, input.Metrics.MaxDrawdownRate, input.Metrics.AverageHoldingSeconds, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *BacktestRepository) GetAnalysis(ctx context.Context, id string) (backtest.SavedAnalysis, error) {
	var session model.BacktestSession
	row := r.db.QueryRowContext(ctx, `SELECT id, token_address, token_symbol, `+"`interval`"+`, start_time, end_time, created_at, updated_at FROM backtest_sessions WHERE id = ?`, id)
	if err := row.Scan(&session.ID, &session.TokenAddress, &session.TokenSymbol, &session.Interval, &session.StartTime, &session.EndTime, &session.CreatedAt, &session.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return backtest.SavedAnalysis{}, errors.New("回测分析不存在")
		}
		return backtest.SavedAnalysis{}, err
	}
	session.StartTime = apptime.InBeijing(session.StartTime)
	session.EndTime = apptime.InBeijing(session.EndTime)
	session.CreatedAt = apptime.InBeijing(session.CreatedAt)
	session.UpdatedAt = apptime.InBeijing(session.UpdatedAt)
	pointRows, err := r.db.QueryContext(ctx, `SELECT side, point_time, input_price, note, matched_kline_time, matched_price FROM backtest_trade_points WHERE session_id = ? ORDER BY point_time ASC`, id)
	if err != nil {
		return backtest.SavedAnalysis{}, err
	}
	defer pointRows.Close()
	points := make([]model.MatchedTradePoint, 0)
	for pointRows.Next() {
		var side string
		var pointTime time.Time
		var inputPrice sql.NullFloat64
		var note sql.NullString
		var matchedTime sql.NullTime
		var matchedPrice sql.NullFloat64
		if err := pointRows.Scan(&side, &pointTime, &inputPrice, &note, &matchedTime, &matchedPrice); err != nil {
			return backtest.SavedAnalysis{}, err
		}
		var price *float64
		if inputPrice.Valid {
			value := inputPrice.Float64
			price = &value
		}
		matchedAt := time.Time{}
		if matchedTime.Valid {
			matchedAt = apptime.InBeijing(matchedTime.Time)
		}
		points = append(points, model.MatchedTradePoint{TradePoint: model.TradePoint{Side: model.TradeSide(side), Time: apptime.InBeijing(pointTime), Price: price, Note: note.String}, MatchedKlineTime: matchedAt, MatchedPrice: matchedPrice.Float64})
	}
	if err := pointRows.Err(); err != nil {
		return backtest.SavedAnalysis{}, err
	}
	tradeRows, err := r.db.QueryContext(ctx, `SELECT profit, profit_rate, holding_seconds, win FROM backtest_trade_results WHERE session_id = ? ORDER BY created_at ASC`, id)
	if err != nil {
		return backtest.SavedAnalysis{}, err
	}
	defer tradeRows.Close()
	trades := make([]model.TradeResult, 0)
	for i := 0; tradeRows.Next(); i++ {
		var trade model.TradeResult
		if err := tradeRows.Scan(&trade.Profit, &trade.ProfitRate, &trade.HoldingSeconds, &trade.Win); err != nil {
			return backtest.SavedAnalysis{}, err
		}
		if i*2+1 < len(points) {
			trade.Buy = points[i*2]
			trade.Sell = points[i*2+1]
		}
		trades = append(trades, trade)
	}
	if err := tradeRows.Err(); err != nil {
		return backtest.SavedAnalysis{}, err
	}
	var metrics model.Metrics
	metricRow := r.db.QueryRowContext(ctx, `SELECT trade_count, win_rate, total_profit_rate, max_drawdown_rate, average_holding_seconds FROM backtest_metric_snapshots WHERE session_id = ?`, id)
	if err := metricRow.Scan(&metrics.TradeCount, &metrics.WinRate, &metrics.TotalProfitRate, &metrics.MaxDrawdownRate, &metrics.AverageHoldingSeconds); err != nil {
		return backtest.SavedAnalysis{}, err
	}
	return backtest.SavedAnalysis{Session: session, Points: points, Trades: trades, Metrics: metrics}, nil
}

func (r *BacktestRepository) ListAnalyses(ctx context.Context, limit int) ([]model.BacktestSession, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, token_address, token_symbol, `+"`interval`"+`, start_time, end_time, created_at, updated_at FROM backtest_sessions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.BacktestSession, 0)
	for rows.Next() {
		var item model.BacktestSession
		if err := rows.Scan(&item.ID, &item.TokenAddress, &item.TokenSymbol, &item.Interval, &item.StartTime, &item.EndTime, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.StartTime = apptime.InBeijing(item.StartTime)
		item.EndTime = apptime.InBeijing(item.EndTime)
		item.CreatedAt = apptime.InBeijing(item.CreatedAt)
		item.UpdatedAt = apptime.InBeijing(item.UpdatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}
