package runtimealert

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type discardNotifier struct{}

func (discardNotifier) NotifyRuntimeAlert(context.Context, Notification) error { return nil }

type fixedSampler struct {
	sample ResourceSample
	err    error
}

func (s fixedSampler) Sample() (ResourceSample, error) { return s.sample, s.err }

type fixedAPIKeyPool struct {
	available int
	total     int
	err       error
}

func (p *fixedAPIKeyPool) APIKeyAvailability(context.Context) (int, int, error) {
	return p.available, p.total, p.err
}

func TestMonitorDoesNotAlertForAuthenticationFailure(t *testing.T) {
	monitor := newTestMonitor()
	monitor.ObserveHTTP("GMGN", http.StatusUnauthorized, 100*time.Millisecond, nil)
	assertNoNotification(t, monitor)
}

func TestMonitorDoesNotAlertForNormalRateLimits(t *testing.T) {
	monitor := newTestMonitor()
	for range 5 {
		monitor.ObserveHTTP("GMGN", http.StatusTooManyRequests, 4*time.Second, nil)
	}
	monitor.ObserveHTTP("GMGN", http.StatusTooManyRequests, 4*time.Second, errors.New("rate-limited response body interrupted"))
	assertNoNotification(t, monitor)
}

func TestMonitorAlertsOnlyWhenAPIKeyAvailabilityIsBelowHalf(t *testing.T) {
	monitor := newTestMonitor()
	pool := &fixedAPIKeyPool{available: 1, total: 3}
	monitor.WithAPIKeyPool("GMGN API Key 池", pool)

	monitor.checkAPIKeyPools(context.Background())
	alert := receiveNotification(t, monitor)
	if alert.Category != "API Key 可用率过低" || alert.Service != "GMGN API Key 池" || alert.Consecutive != 1 || alert.Recovery {
		t.Fatalf("unexpected alert: %#v", alert)
	}

	pool.available = 2
	monitor.checkAPIKeyPools(context.Background())
	assertNoNotification(t, monitor)
	monitor.checkAPIKeyPools(context.Background())
	recovery := receiveNotification(t, monitor)
	if !recovery.Recovery || recovery.Category != "API Key 可用率过低" {
		t.Fatalf("unexpected recovery: %#v", recovery)
	}
}

func TestMonitorDoesNotAlertAtExactlyHalfAvailability(t *testing.T) {
	monitor := newTestMonitor()
	monitor.WithAPIKeyPool("GMGN API Key 池", &fixedAPIKeyPool{available: 1, total: 2})
	monitor.checkAPIKeyPools(context.Background())
	assertNoNotification(t, monitor)
}

func TestMonitorAlertsForNetworkFailureAndLatency(t *testing.T) {
	monitor := newTestMonitor()
	for range 3 {
		monitor.ObserveHTTP("Jupiter", 0, 100*time.Millisecond, errors.New("dial timeout"))
	}
	failure := receiveNotification(t, monitor)
	if failure.Category != "外部接口连续失败" {
		t.Fatalf("unexpected failure alert: %#v", failure)
	}
	for range 3 {
		monitor.ObserveHTTP("Jupiter", http.StatusOK, 4*time.Second, nil)
	}
	latency := receiveNotification(t, monitor)
	if latency.Recovery {
		latency = receiveNotification(t, monitor)
	}
	if latency.Category != "外部接口延迟过高" {
		t.Fatalf("unexpected latency alert: %#v", latency)
	}
}

func TestMonitorAlertsAfterConsecutiveHighResourceSamples(t *testing.T) {
	monitor := New(Config{Enabled: true, ResourceConsecutiveSamples: 3}, discardNotifier{}, fixedSampler{sample: ResourceSample{CPUPercent: 90, MemoryPercent: 91}})
	monitor.checkResources()
	monitor.checkResources()
	assertNoNotification(t, monitor)
	monitor.checkResources()
	first := receiveNotification(t, monitor)
	second := receiveNotification(t, monitor)
	categories := map[string]bool{first.Category: true, second.Category: true}
	if !categories["服务器 CPU 过高"] || !categories["服务器内存过高"] {
		t.Fatalf("unexpected resource alerts: %#v %#v", first, second)
	}
}

func newTestMonitor() *Monitor {
	return New(Config{
		Enabled:                true,
		ConsecutiveFailures:    3,
		ConsecutiveHighLatency: 3,
		RecoverySuccesses:      2,
		LatencyThreshold:       3 * time.Second,
	}, discardNotifier{}, fixedSampler{})
}

func receiveNotification(t *testing.T, monitor *Monitor) Notification {
	t.Helper()
	select {
	case notification := <-monitor.queue:
		return notification
	default:
		t.Fatal("expected notification")
		return Notification{}
	}
}

func assertNoNotification(t *testing.T, monitor *Monitor) {
	t.Helper()
	select {
	case notification := <-monitor.queue:
		t.Fatalf("unexpected notification: %#v", notification)
	default:
	}
}
