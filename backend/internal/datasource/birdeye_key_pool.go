package datasource

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

type birdeyeAPIError struct {
	statusCode int
	message    string
}

func (e *birdeyeAPIError) Error() string {
	if strings.TrimSpace(e.message) != "" {
		return e.message
	}
	if e.statusCode > 0 {
		return http.StatusText(e.statusCode)
	}
	return "Birdeye 请求失败"
}

func (e *birdeyeAPIError) Retryable() bool {
	if e == nil {
		return false
	}
	if e.statusCode == http.StatusTooManyRequests {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(e.message))
	return strings.Contains(message, "compute units usage limit exceeded") ||
		strings.Contains(message, "credits is not enough") ||
		strings.Contains(message, "rate limit") ||
		strings.Contains(message, "too many requests") ||
		strings.Contains(message, "quota")
}

type birdeyeFailureBody struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func nextBirdeyeKey(keys []string, cursor *uint32) string {
	if len(keys) == 0 {
		return ""
	}
	index := int(atomic.AddUint32(cursor, 1)-1) % len(keys)
	return keys[index]
}

func decodeBirdeyeBody(resp *http.Response, successTarget any) error {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &birdeyeAPIError{statusCode: resp.StatusCode}
		var failure birdeyeFailureBody
		if err := json.Unmarshal(bodyBytes, &failure); err == nil {
			apiErr.message = strings.TrimSpace(failure.Message)
		}
		return apiErr
	}
	if err := json.Unmarshal(bodyBytes, successTarget); err != nil {
		return err
	}
	return nil
}

func birdeyeBodyError(message string, fallback string) error {
	apiErr := &birdeyeAPIError{message: strings.TrimSpace(message)}
	if apiErr.message == "" {
		apiErr.message = fallback
	}
	return apiErr
}

func shouldRetryBirdeye(err error) bool {
	var apiErr *birdeyeAPIError
	return errors.As(err, &apiErr) && apiErr.Retryable()
}
