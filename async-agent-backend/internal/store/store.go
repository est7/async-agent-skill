package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"async-agent-backend/internal/task"
)

const (
	envTaskRoot = "ASYNC_AGENT_TASK_DIR"
)

// Store persists task metadata and task outputs on the local filesystem.
type Store struct {
	root string
}

// New creates a task store rooted at the configured task directory.
func New(root string) (*Store, error) {
	resolved, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return nil, fmt.Errorf("create task root: %w", err)
	}
	return &Store{root: resolved}, nil
}

// Root returns the absolute task root path.
func (s *Store) Root() string {
	return s.root
}

// Create initializes the task directory and default file locations for a task.
func (s *Store) Create(rec task.Record) (task.Record, error) {
	taskDir := s.taskDir(rec.TaskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return task.Record{}, fmt.Errorf("create task dir: %w", err)
	}
	rec.StdoutPath = filepath.Join(taskDir, "stdout.log")
	rec.StderrPath = filepath.Join(taskDir, "stderr.log")
	rec.ResultPath = filepath.Join(taskDir, "result.txt")
	rec.NormalizedResultPath = filepath.Join(taskDir, "normalized_result.json")

	if err := ensureEmptyFile(rec.StdoutPath); err != nil {
		return task.Record{}, err
	}
	if err := ensureEmptyFile(rec.StderrPath); err != nil {
		return task.Record{}, err
	}
	if err := ensureEmptyFile(rec.ResultPath); err != nil {
		return task.Record{}, err
	}
	if err := ensureEmptyFile(rec.NormalizedResultPath); err != nil {
		return task.Record{}, err
	}
	if err := s.Save(rec); err != nil {
		return task.Record{}, err
	}
	return rec, nil
}

// Save writes task metadata atomically.
func (s *Store) Save(rec task.Record) error {
	metaPath := s.metaPath(rec.TaskID)
	metaDir := filepath.Dir(metaPath)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return fmt.Errorf("create metadata dir: %w", err)
	}
	payload, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	tmpFile, err := os.CreateTemp(metaDir, "meta.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp metadata: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmpFile.Write(payload); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write tmp metadata: %w", err)
	}
	if err := tmpFile.Chmod(0o644); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod tmp metadata: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close tmp metadata: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("replace metadata: %w", err)
	}
	return nil
}

// Load reads task metadata from disk.
func (s *Store) Load(taskID string) (task.Record, error) {
	data, err := os.ReadFile(s.metaPath(taskID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return task.Record{}, fmt.Errorf("task %s not found", taskID)
		}
		return task.Record{}, fmt.Errorf("read metadata: %w", err)
	}
	var rec task.Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return task.Record{}, fmt.Errorf("decode metadata: %w", err)
	}
	return rec, nil
}

// WriteResult stores the extracted provider result.
func (s *Store) WriteResult(taskID string, result string) error {
	if err := os.WriteFile(s.resultPath(taskID), []byte(result), 0o644); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}

// ReadResult returns the extracted provider result.
func (s *Store) ReadResult(taskID string) (string, error) {
	bytes, err := os.ReadFile(s.resultPath(taskID))
	if err != nil {
		return "", fmt.Errorf("read result: %w", err)
	}
	return string(bytes), nil
}

// WriteNormalizedResult stores the normalized result envelope.
func (s *Store) WriteNormalizedResult(taskID string, result task.NormalizedResult) error {
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal normalized result: %w", err)
	}
	if err := os.WriteFile(s.normalizedResultPath(taskID), payload, 0o644); err != nil {
		return fmt.Errorf("write normalized result: %w", err)
	}
	return nil
}

// ReadNormalizedResult loads the normalized result envelope for a task.
func (s *Store) ReadNormalizedResult(taskID string) (*task.NormalizedResult, error) {
	bytes, err := os.ReadFile(s.normalizedResultPath(taskID))
	if err != nil {
		return nil, fmt.Errorf("read normalized result: %w", err)
	}
	trimmed := string(bytes)
	if trimmed == "" {
		return nil, nil
	}
	var result task.NormalizedResult
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, fmt.Errorf("decode normalized result: %w", err)
	}
	if result.OutputMode == "" && result.FinalText == "" && result.StructuredOutput == nil && result.StreamEventCount == 0 {
		return nil, nil
	}
	return &result, nil
}

// ReadLogs returns the persisted stdout/stderr output for a task.
func (s *Store) ReadLogs(taskID string) (task.LogsPayload, error) {
	rec, err := s.Load(taskID)
	if err != nil {
		return task.LogsPayload{}, err
	}
	stdout, err := os.ReadFile(rec.StdoutPath)
	if err != nil {
		return task.LogsPayload{}, fmt.Errorf("read stdout: %w", err)
	}
	stderr, err := os.ReadFile(rec.StderrPath)
	if err != nil {
		return task.LogsPayload{}, fmt.Errorf("read stderr: %w", err)
	}
	return task.LogsPayload{TaskID: taskID, Stdout: string(stdout), Stderr: string(stderr)}, nil
}

// TouchActivity records the latest output activity time for a task without overwriting newer task state.
func (s *Store) TouchActivity(rec task.Record) (task.Record, error) {
	latest, err := latestModTime(rec.StdoutPath, rec.StderrPath)
	if err != nil {
		return task.Record{}, err
	}
	if latest.IsZero() {
		return rec, nil
	}
	latestRec, err := s.Load(rec.TaskID)
	if err != nil {
		return task.Record{}, err
	}
	if latestRec.LastActivityAt != nil && !latest.After(*latestRec.LastActivityAt) {
		return latestRec, nil
	}
	latestRec.LastActivityAt = &latest
	if err := s.Save(latestRec); err != nil {
		return task.Record{}, err
	}
	return latestRec, nil
}

func (s *Store) taskDir(taskID string) string {
	return filepath.Join(s.root, taskID)
}

func (s *Store) metaPath(taskID string) string {
	return filepath.Join(s.taskDir(taskID), "meta.json")
}

func (s *Store) resultPath(taskID string) string {
	return filepath.Join(s.taskDir(taskID), "result.txt")
}

func (s *Store) normalizedResultPath(taskID string) string {
	return filepath.Join(s.taskDir(taskID), "normalized_result.json")
}

func ensureEmptyFile(path string) error {
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		return fmt.Errorf("create file %s: %w", path, err)
	}
	return nil
}

func resolveRoot(root string) (string, error) {
	if root != "" {
		return filepath.Abs(root)
	}
	if fromEnv := os.Getenv(envTaskRoot); fromEnv != "" {
		return filepath.Abs(fromEnv)
	}
	cacheDir, err := os.UserCacheDir()
	if err == nil {
		return filepath.Abs(filepath.Join(cacheDir, "async-agent-backend", "tasks"))
	}
	return filepath.Abs(filepath.Join(os.TempDir(), "async-agent-backend", "tasks"))
}

func latestModTime(paths ...string) (time.Time, error) {
	var latest time.Time
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return time.Time{}, fmt.Errorf("stat %s: %w", path, err)
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest, nil
}
