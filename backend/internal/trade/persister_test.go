package trade

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestAsyncPersisterFailedTaskDoesNotBlockFollowingTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	persister := &asyncPersister{ch: make(chan persistTask, 8), retryDelay: 5 * time.Millisecond}
	go persister.loop(ctx)

	var poisonAttempts atomic.Int32
	persister.Enqueue(persistTask{name: "poison", run: func(context.Context) error {
		poisonAttempts.Add(1)
		return errors.New("permanent failure")
	}})
	done := make(chan struct{})
	persister.Enqueue(persistTask{name: "following", run: func(context.Context) error {
		close(done)
		return nil
	}})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("following task was blocked by a failed task")
	}
	if poisonAttempts.Load() == 0 {
		t.Fatal("expected failed task to run")
	}
}
