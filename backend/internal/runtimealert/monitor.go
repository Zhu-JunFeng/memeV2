package runtimealert

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type Config struct {
	Enabled                    bool
	LatencyThreshold           time.Duration
	ConsecutiveFailures        int
	ConsecutiveRateLimits      int
	ConsecutiveHighLatency     int
	RecoverySuccesses          int
	Cooldown                   time.Duration
	ResourceCheckInterval      time.Duration
	ResourceConsecutiveSamples int
	CPUThresholdPercent        float64
	MemoryThresholdPercent     float64
}

type Notification struct {
	Recovery    bool
	Category    string
	Service     string
	Detail      string
	Consecutive int
	OccurredAt  time.Time
}

type Notifier interface {
	NotifyRuntimeAlert(ctx context.Context, notification Notification) error
}

type ResourceSample struct {
	CPUPercent    float64
	MemoryPercent float64
}

type ResourceSampler interface {
	Sample() (ResourceSample, error)
}

type issueState struct {
	consecutive int
	healthy     int
	active      bool
	lastAlert   time.Time
}

type Monitor struct {
	cfg      Config
	notifier Notifier
	sampler  ResourceSampler
	now      func() time.Time

	mu     sync.Mutex
	issues map[string]*issueState
	queue  chan Notification
}

func New(cfg Config, notifier Notifier, sampler ResourceSampler) *Monitor {
	cfg = normalizeConfig(cfg)
	if sampler == nil {
		sampler = NewLinuxResourceSampler()
	}
	return &Monitor{
		cfg:      cfg,
		notifier: notifier,
		sampler:  sampler,
		now:      time.Now,
		issues:   make(map[string]*issueState),
		queue:    make(chan Notification, 64),
	}
}

func normalizeConfig(cfg Config) Config {
	if cfg.LatencyThreshold <= 0 {
		cfg.LatencyThreshold = 3 * time.Second
	}
	if cfg.ConsecutiveFailures <= 0 {
		cfg.ConsecutiveFailures = 3
	}
	if cfg.ConsecutiveRateLimits <= 0 {
		cfg.ConsecutiveRateLimits = 3
	}
	if cfg.ConsecutiveHighLatency <= 0 {
		cfg.ConsecutiveHighLatency = 3
	}
	if cfg.RecoverySuccesses <= 0 {
		cfg.RecoverySuccesses = 2
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 10 * time.Minute
	}
	if cfg.ResourceCheckInterval <= 0 {
		cfg.ResourceCheckInterval = 30 * time.Second
	}
	if cfg.ResourceConsecutiveSamples <= 0 {
		cfg.ResourceConsecutiveSamples = 3
	}
	if cfg.CPUThresholdPercent <= 0 {
		cfg.CPUThresholdPercent = 85
	}
	if cfg.MemoryThresholdPercent <= 0 {
		cfg.MemoryThresholdPercent = 85
	}
	return cfg
}

func (m *Monitor) Start(ctx context.Context) {
	if m == nil || !m.cfg.Enabled || m.notifier == nil {
		return
	}
	log.Printf("runtime alert monitor started: latency=%s cpu=%.1f%% memory=%.1f%%", m.cfg.LatencyThreshold, m.cfg.CPUThresholdPercent, m.cfg.MemoryThresholdPercent)
	go m.runNotifier(ctx)
	go m.runResourceChecks(ctx)
}

func (m *Monitor) ObserveHTTP(service string, statusCode int, duration time.Duration, requestErr error) {
	if m == nil || !m.cfg.Enabled || m.notifier == nil {
		return
	}
	now := m.now()
	authFailed := statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
	rateLimited := statusCode == http.StatusTooManyRequests
	requestFailed := requestErr != nil || (statusCode >= 400 && !authFailed && !rateLimited)
	highLatency := duration >= m.cfg.LatencyThreshold

	m.observe(service+":auth", authFailed, 1, Notification{
		Category: "外部接口鉴权失败", Service: service,
		Detail: fmt.Sprintf("HTTP %d，API Key 可能失效或权限不足", statusCode), OccurredAt: now,
	})
	m.observe(service+":rate_limit", rateLimited, m.cfg.ConsecutiveRateLimits, Notification{
		Category: "外部接口频繁限流", Service: service,
		Detail: fmt.Sprintf("连续收到 HTTP %d", statusCode), OccurredAt: now,
	})
	failureDetail := fmt.Sprintf("HTTP %d", statusCode)
	if requestErr != nil {
		failureDetail = requestErr.Error()
	}
	m.observe(service+":failure", requestFailed, m.cfg.ConsecutiveFailures, Notification{
		Category: "外部接口连续失败", Service: service, Detail: failureDetail, OccurredAt: now,
	})
	m.observe(service+":latency", highLatency, m.cfg.ConsecutiveHighLatency, Notification{
		Category: "外部接口延迟过高", Service: service,
		Detail: fmt.Sprintf("本次耗时 %s，阈值 %s", duration.Round(time.Millisecond), m.cfg.LatencyThreshold), OccurredAt: now,
	})
}

func (m *Monitor) observe(key string, unhealthy bool, threshold int, notification Notification) {
	m.mu.Lock()
	state := m.issues[key]
	if state == nil {
		state = &issueState{}
		m.issues[key] = state
	}
	now := notification.OccurredAt
	if unhealthy {
		state.healthy = 0
		state.consecutive++
		if state.consecutive >= threshold && (!state.active || now.Sub(state.lastAlert) >= m.cfg.Cooldown) {
			state.active = true
			state.lastAlert = now
			notification.Consecutive = state.consecutive
			m.mu.Unlock()
			m.enqueue(notification)
			return
		}
		m.mu.Unlock()
		return
	}

	state.consecutive = 0
	if !state.active {
		state.healthy = 0
		m.mu.Unlock()
		return
	}
	state.healthy++
	if state.healthy < m.cfg.RecoverySuccesses {
		m.mu.Unlock()
		return
	}
	state.active = false
	state.healthy = 0
	notification.Recovery = true
	notification.Detail = "连续检测恢复正常"
	notification.Consecutive = m.cfg.RecoverySuccesses
	m.mu.Unlock()
	m.enqueue(notification)
}

func (m *Monitor) enqueue(notification Notification) {
	select {
	case m.queue <- notification:
	default:
		log.Printf("runtime alert queue full, dropped: category=%s service=%s", notification.Category, notification.Service)
	}
}

func (m *Monitor) runNotifier(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case notification := <-m.queue:
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := m.notifier.NotifyRuntimeAlert(sendCtx, notification)
			cancel()
			if err != nil {
				log.Printf("send runtime alert failed: category=%s service=%s err=%v", notification.Category, notification.Service, err)
			} else {
				log.Printf("runtime alert sent: recovery=%t category=%s service=%s", notification.Recovery, notification.Category, notification.Service)
			}
		}
	}
}

func (m *Monitor) runResourceChecks(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.ResourceCheckInterval)
	defer ticker.Stop()
	m.checkResources()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkResources()
		}
	}
}

func (m *Monitor) checkResources() {
	sample, err := m.sampler.Sample()
	if err != nil {
		log.Printf("sample server resources failed: %v", err)
		return
	}
	now := m.now()
	m.observe("server:cpu", sample.CPUPercent >= m.cfg.CPUThresholdPercent, m.cfg.ResourceConsecutiveSamples, Notification{
		Category: "服务器 CPU 过高", Service: "生产服务器",
		Detail: fmt.Sprintf("当前 %.1f%%，阈值 %.1f%%", sample.CPUPercent, m.cfg.CPUThresholdPercent), OccurredAt: now,
	})
	m.observe("server:memory", sample.MemoryPercent >= m.cfg.MemoryThresholdPercent, m.cfg.ResourceConsecutiveSamples, Notification{
		Category: "服务器内存过高", Service: "生产服务器",
		Detail: fmt.Sprintf("当前 %.1f%%，阈值 %.1f%%", sample.MemoryPercent, m.cfg.MemoryThresholdPercent), OccurredAt: now,
	})
}
