package httpclient

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HTTPObserver interface {
	ObserveHTTP(service string, statusCode int, duration time.Duration, requestErr error)
}

type observedTransport struct {
	service  string
	base     http.RoundTripper
	observer HTTPObserver
}

func WithObserver(client *http.Client, service string, observer HTTPObserver) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	cloned := *client
	base := cloned.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cloned.Transport = &observedTransport{service: service, base: base, observer: observer}
	return &cloned
}

func (t *observedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	startedAt := time.Now()
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil {
		if t.observer != nil {
			t.observer.ObserveHTTP(t.service, 0, time.Since(startedAt), err)
		}
		return response, err
	}
	response.Body = &observedBody{
		ReadCloser: response.Body,
		statusCode: response.StatusCode,
		observe: func(statusCode int, readErr error) {
			if t.observer != nil {
				t.observer.ObserveHTTP(t.service, statusCode, time.Since(startedAt), readErr)
			}
		},
	}
	return response, err
}

type observedBody struct {
	io.ReadCloser
	once       sync.Once
	readErr    error
	statusCode int
	prefix     bytes.Buffer
	observe    func(int, error)
}

func (b *observedBody) Read(buffer []byte) (int, error) {
	count, err := b.ReadCloser.Read(buffer)
	if count > 0 && b.prefix.Len() < 8192 {
		remaining := 8192 - b.prefix.Len()
		if count < remaining {
			remaining = count
		}
		_, _ = b.prefix.Write(buffer[:remaining])
	}
	if err != nil && err != io.EOF {
		b.readErr = err
	}
	return count, err
}

func (b *observedBody) Close() error {
	closeErr := b.ReadCloser.Close()
	observeErr := b.readErr
	if observeErr == nil {
		observeErr = closeErr
	}
	statusCode := effectiveStatusCode(b.statusCode, b.prefix.Bytes())
	b.once.Do(func() { b.observe(statusCode, observeErr) })
	return closeErr
}

func effectiveStatusCode(httpStatus int, payload []byte) int {
	if httpStatus < 200 || httpStatus >= 300 || len(payload) == 0 {
		return httpStatus
	}
	var envelope struct {
		Code any `json:"code"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.Code == nil {
		return httpStatus
	}
	code := 0
	switch value := envelope.Code.(type) {
	case float64:
		code = int(value)
	case string:
		code, _ = strconv.Atoi(strings.TrimSpace(value))
	}
	if code >= 400 && code <= 599 {
		return code
	}
	return httpStatus
}
