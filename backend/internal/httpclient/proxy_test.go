package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestNewDirectClientDoesNotConfigureProxy(t *testing.T) {
	client := NewDirectClient(time.Second, time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct HTTP client without proxy")
	}
}
