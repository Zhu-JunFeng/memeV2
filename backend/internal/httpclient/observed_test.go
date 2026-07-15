package httpclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type captureObserver struct {
	service  string
	status   int
	err      error
	duration time.Duration
}

func (o *captureObserver) ObserveHTTP(service string, statusCode int, duration time.Duration, requestErr error) {
	o.service = service
	o.status = statusCode
	o.duration = duration
	o.err = requestErr
}

func TestWithObserverPreservesResponseAndReportsOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	observer := &captureObserver{}
	client := WithObserver(&http.Client{Timeout: time.Second}, "GMGN", observer)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests || observer.status != http.StatusTooManyRequests || observer.service != "GMGN" {
		t.Fatalf("unexpected observation: response=%d observer=%#v", response.StatusCode, observer)
	}
	if observer.duration <= 0 || observer.err != nil {
		t.Fatalf("unexpected duration/error: %#v", observer)
	}
}

func TestWithObserverRecognizesApplicationRateLimitCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":429,"message":"rate limited"}`))
	}))
	defer server.Close()
	observer := &captureObserver{}
	client := WithObserver(&http.Client{Timeout: time.Second}, "GMGN", observer)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || observer.status != http.StatusTooManyRequests {
		t.Fatalf("expected HTTP response unchanged and observed application 429, response=%d observer=%#v", response.StatusCode, observer)
	}
}
