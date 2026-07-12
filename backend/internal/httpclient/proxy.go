package httpclient

import (
	"net"
	"net/http"
	"time"
)

// NewDirectClient 为外部服务创建不经过代理的 HTTP 客户端。
func NewDirectClient(timeout time.Duration, dialTimeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: dialTimeout}).DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}
}
