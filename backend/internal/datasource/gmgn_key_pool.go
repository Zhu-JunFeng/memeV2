package datasource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
)

var ErrGMGNNoAvailableKey = errors.New("GMGN 可用 API Key 不存在")

type GMGNKeyPool interface {
	ListAvailableGMGNKeys(ctx context.Context) ([]string, error)
	MarkGMGNKeySuccessful(ctx context.Context, apiKey string) error
}

type gmgnAPIError struct {
	statusCode int
	code       int
	message    string
}

func (e *gmgnAPIError) Error() string {
	message := strings.TrimSpace(e.message)
	if message != "" {
		return message
	}
	if e.statusCode > 0 {
		return fmt.Sprintf("HTTP %d", e.statusCode)
	}
	if e.code > 0 {
		return fmt.Sprintf("code %d", e.code)
	}
	return "GMGN 请求失败"
}

func (e *gmgnAPIError) Retryable() bool {
	if e == nil {
		return false
	}
	if e.statusCode == http.StatusTooManyRequests || e.code == http.StatusTooManyRequests {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(e.message))
	return strings.Contains(message, "rate limit") ||
		strings.Contains(message, "too many requests") ||
		strings.Contains(message, "quota") ||
		strings.Contains(message, "限流")
}

func nextGMGNKey(keys []string, cursor *uint32) string {
	if len(keys) == 0 {
		return ""
	}
	index := int(atomic.AddUint32(cursor, 1)-1) % len(keys)
	return keys[index]
}

func shouldRetryGMGN(err error) bool {
	var apiErr *gmgnAPIError
	return errors.As(err, &apiErr) && apiErr.Retryable()
}
