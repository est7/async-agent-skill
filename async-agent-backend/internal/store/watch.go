package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

var watchedTaskFiles = map[string]struct{}{
	"stdout.log":             {},
	"stderr.log":             {},
	"result.txt":             {},
	"normalized_result.json": {},
	"meta.json":              {},
	"meta.json.tmp":          {},
}

// TaskWatcher waits for task-directory filesystem activity and falls back to a timer.
type TaskWatcher struct {
	taskDir string
	watcher *fsnotify.Watcher
}

// WatchTask opens a filesystem watcher for the task directory.
func (s *Store) WatchTask(taskID string) (*TaskWatcher, error) {
	taskDir := s.taskDir(taskID)
	if _, err := os.Stat(taskDir); err != nil {
		return nil, fmt.Errorf("stat task dir: %w", err)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create task watcher: %w", err)
	}
	if err := watcher.Add(taskDir); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("watch task dir: %w", err)
	}
	return &TaskWatcher{taskDir: filepath.Clean(taskDir), watcher: watcher}, nil
}

// Close releases the underlying filesystem watcher.
func (w *TaskWatcher) Close() error {
	if w == nil || w.watcher == nil {
		return nil
	}
	return w.watcher.Close()
}

// Wait blocks until a task file changes, the fallback interval elapses, or the context is done.
func (w *TaskWatcher) Wait(ctx context.Context, fallback time.Duration) error {
	if w == nil || w.watcher == nil {
		return waitForFallback(ctx, fallback)
	}
	timer := time.NewTimer(fallback)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
			if err != nil {
				return nil
			}
		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			if w.isRelevant(event.Name) {
				return nil
			}
		}
	}
}

func (w *TaskWatcher) isRelevant(name string) bool {
	cleanName := filepath.Clean(name)
	if cleanName == w.taskDir {
		return true
	}
	_, ok := watchedTaskFiles[filepath.Base(cleanName)]
	return ok
}

func waitForFallback(ctx context.Context, fallback time.Duration) error {
	timer := time.NewTimer(fallback)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
