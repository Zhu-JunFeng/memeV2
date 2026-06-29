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
	ch chan persistTask
}

func newAsyncPersister(ctx context.Context, size int) *asyncPersister {
	if size <= 0 {
		size = 256
	}
	p := &asyncPersister{ch: make(chan persistTask, size)}
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
			p.runWithRetry(ctx, task)
		}
	}
}

func (p *asyncPersister) runWithRetry(ctx context.Context, task persistTask) {
	for {
		if err := task.run(ctx); err == nil {
			return
		} else {
			log.Printf("trade async persist failed: task=%s err=%v", task.name, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
