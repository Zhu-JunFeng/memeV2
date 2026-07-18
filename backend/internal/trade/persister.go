package trade

import (
	"context"
	"log"
	"time"
)

type persistTask struct {
	name string
	run  func(context.Context) error
}

type asyncPersister struct {
	ch         chan persistTask
	retryDelay time.Duration
}

func newAsyncPersister(ctx context.Context, size int) *asyncPersister {
	if size <= 0 {
		size = 256
	}
	p := &asyncPersister{ch: make(chan persistTask, size), retryDelay: 2 * time.Second}
	go p.loop(ctx)
	return p
}

func (p *asyncPersister) Enqueue(task persistTask) {
	if p == nil || task.run == nil {
		return
	}
	p.ch <- task
}

func (p *asyncPersister) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-p.ch:
			p.runOrRetry(ctx, task)
		}
	}
}

func (p *asyncPersister) runOrRetry(ctx context.Context, task persistTask) {
	if err := task.run(ctx); err != nil {
		log.Printf("trade async persist failed and scheduled for retry: task=%s err=%v", task.name, err)
		go p.scheduleRetry(ctx, task)
	}
}

func (p *asyncPersister) scheduleRetry(ctx context.Context, task persistTask) {
	timer := time.NewTimer(p.retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	select {
	case <-ctx.Done():
	case p.ch <- task:
	}
}
