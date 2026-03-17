package runner

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"async-agent-backend/internal/store"
	"async-agent-backend/internal/task"
)

func TestWaitForTerminalUsesFilesystemWakeupBeforeFallback(t *testing.T) {
	t.Parallel()

	taskStore, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	run, err := New(taskStore, "/bin/echo", slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new runner: %v", err)
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

	errCh := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		completedAt := time.Now()
		rec.Status = task.StatusSucceeded
		rec.CompletedAt = &completedAt
		errCh <- taskStore.Save(rec)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	startedAt := time.Now()
	result, err := run.waitForTerminal(ctx, rec.TaskID, 5*time.Second)
	if err != nil {
		t.Fatalf("wait for terminal status: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("save terminal record: %v", err)
	}
	if result.Status != task.StatusSucceeded {
		t.Fatalf("expected succeeded status, got %s", result.Status)
	}
	if elapsed := time.Since(startedAt); elapsed >= time.Second {
		t.Fatalf("expected filesystem wake-up before fallback, got %v", elapsed)
	}
}
