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

func TestMonitorAlertsImmediatelyForAuthenticationFailure(t *testing.T) {
	monitor := newTestMonitor()
	monitor.ObserveHTTP("GMGN", http.StatusUnauthorized, 100*time.Millisecond, nil)
	notification := receiveNotification(t, monitor)
	if notification.Category != "外部接口鉴权失败" || notification.Consecutive != 1 {
		t.Fatalf("unexpected notification: %#v", notification)
	}
}

func TestMonitorRateLimitRequiresConsecutiveFailuresAndRecovers(t *testing.T) {
	monitor := newTestMonitor()
	monitor.ObserveHTTP("GMGN", http.StatusTooManyRequests, 100*time.Millisecond, nil)
	monitor.ObserveHTTP("GMGN", http.StatusTooManyRequests, 100*time.Millisecond, nil)
	assertNoNotification(t, monitor)
	monitor.ObserveHTTP("GMGN", http.StatusTooManyRequests, 100*time.Millisecond, nil)
	alert := receiveNotification(t, monitor)
	if alert.Category != "外部接口频繁限流" || alert.Consecutive != 3 || alert.Recovery {
		t.Fatalf("unexpected alert: %#v", alert)
	}
	monitor.ObserveHTTP("GMGN", http.StatusOK, 100*time.Millisecond, nil)
	assertNoNotification(t, monitor)
	monitor.ObserveHTTP("GMGN", http.StatusOK, 100*time.Millisecond, nil)
	recovery := receiveNotification(t, monitor)
	if !recovery.Recovery || recovery.Category != "外部接口频繁限流" {
		t.Fatalf("unexpected recovery: %#v", recovery)
	}
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
		ConsecutiveRateLimits:  3,
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
