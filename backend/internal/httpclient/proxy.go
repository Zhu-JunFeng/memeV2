package httpclient

import (
	"net"
	"net/http"
	"net/url"
	"time"
)

const FixedClashProxyURL = "http://127.0.0.1:7890"

// NewFixedProxyClient 为 DexScreener/Jupiter 这类外网依赖统一走服务器本机 clash 代理。
func NewFixedProxyClient(timeout time.Duration, dialTimeout time.Duration) *http.Client {
	proxyURL, err := url.Parse(FixedClashProxyURL)
	if err != nil {
		panic(err)
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxyURL),
			DialContext:         (&net.Dialer{Timeout: dialTimeout}).DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}
}
