package store

import (
	"context"
	"testing"
	"time"

	"async-agent-backend/internal/task"
)

func TestTaskWatcherWaitWakesOnTaskDirectoryChange(t *testing.T) {
	t.Parallel()

	taskStore, rec := newTestStoreAndRecord(t)
	watcher, err := taskStore.WatchTask(rec.TaskID)
	if err != nil {
		t.Fatalf("watch task: %v", err)
	}
	defer watcher.Close()

	errCh := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		completedAt := time.Now()
		rec.Status = task.StatusSucceeded
		rec.CompletedAt = &completedAt
		errCh <- taskStore.Save(rec)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	if err := watcher.Wait(ctx, 5*time.Second); err != nil {
		t.Fatalf("wait for file event: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("save task metadata: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed >= time.Second {
		t.Fatalf("expected filesystem wake-up before fallback, got %v", elapsed)
	}
}

func TestTaskWatcherWaitFallsBackWithoutChanges(t *testing.T) {
	t.Parallel()

	taskStore, rec := newTestStoreAndRecord(t)
	watcher, err := taskStore.WatchTask(rec.TaskID)
	if err != nil {
		t.Fatalf("watch task: %v", err)
	}
	defer watcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	if err := watcher.Wait(ctx, 75*time.Millisecond); err != nil {
		t.Fatalf("wait for fallback interval: %v", err)
	}
	elapsed := time.Since(startedAt)
	if elapsed < 50*time.Millisecond {
		t.Fatalf("expected wait to respect fallback interval, got %v", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected fallback wait to return promptly, got %v", elapsed)
	}
}

func newTestStoreAndRecord(t *testing.T) (*Store, task.Record) {
	t.Helper()

	taskStore, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	rec, err := taskStore.Create(task.Record{
		TaskID: "task-1",
		Request: task.Request{
			Provider: "claude",
		},
		Status: task.StatusRunning,
	})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	return taskStore, rec
}
