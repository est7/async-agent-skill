package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"async-agent-backend/internal/providers"
	"async-agent-backend/internal/store"
	"async-agent-backend/internal/task"
)

const (
	pollInterval      = 500 * time.Millisecond
	cancelGracePeriod = 5 * time.Second
	statusRepairDelay = 250 * time.Millisecond
	statusRepairTries = 10
)

// Runner owns task submission, worker execution and state inspection.
type Runner struct {
	store      *store.Store
	logger     *slog.Logger
	executable string
}

// New creates a runner for the provided store and executable path.
func New(taskStore *store.Store, executable string, logger *slog.Logger) (*Runner, error) {
	if taskStore == nil {
		return nil, errors.New("task store is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
	}
	return &Runner{store: taskStore, logger: logger, executable: executable}, nil
}

// Submit starts a detached worker process that will execute the requested provider.
func (r *Runner) Submit(req task.Request) (task.Record, error) {
	rec, err := r.prepareRecord(req)
	if err != nil {
		return task.Record{}, err
	}
	cmd := exec.Command(r.executable, "worker", "--task-id", rec.TaskID, "--store-root", r.store.Root())
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return task.Record{}, fmt.Errorf("start worker: %w", err)
	}
	rec.WorkerPID = cmd.Process.Pid
	rec.StartedAt = timePtr(time.Now())
	rec.Status = task.StatusRunning
	if err := r.store.Save(rec); err != nil {
		return task.Record{}, err
	}
	r.logger.Info("task submitted", "task_id", rec.TaskID, "provider", req.Provider, "worker_pid", rec.WorkerPID)
	return rec, nil
}

// Run executes a task synchronously in-process and returns the final status.
func (r *Runner) Run(req task.Request) (task.ResultPayload, error) {
	rec, err := r.prepareRecord(req)
	if err != nil {
		return task.ResultPayload{}, err
	}
	if err := r.RunWorker(rec.TaskID); err != nil {
		status, statusErr := r.Status(rec.TaskID)
		if statusErr == nil {
			return status, nil
		}
		return task.ResultPayload{}, err
	}
	return r.Status(rec.TaskID)
}

func (r *Runner) prepareRecord(req task.Request) (task.Record, error) {
	if strings.TrimSpace(req.Provider) == "" {
		return task.Record{}, errors.New("provider is required")
	}
	provider, err := providers.Registry(req.Provider)
	if err != nil {
		return task.Record{}, err
	}
	if err := provider.Validate(req); err != nil {
		return task.Record{}, err
	}
	if req.WorkingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return task.Record{}, fmt.Errorf("resolve working directory: %w", err)
		}
		req.WorkingDir = cwd
	} else {
		req.WorkingDir, err = filepath.Abs(req.WorkingDir)
		if err != nil {
			return task.Record{}, fmt.Errorf("resolve working directory: %w", err)
		}
	}
	taskID := newTaskID()
	rec := task.Record{
		TaskID:  taskID,
		Request: req,
		Status:  task.StatusQueued,
	}
	rec, err = r.store.Create(rec)
	if err != nil {
		return task.Record{}, err
	}
	return rec, nil
}

// Status returns the latest task state and repairs unfinished metadata after worker crashes.
func (r *Runner) Status(taskID string) (task.ResultPayload, error) {
	rec, err := r.store.Load(taskID)
	if err != nil {
		return task.ResultPayload{}, err
	}
	if !rec.Terminal() && rec.WorkerPID != 0 {
		alive, err := processAlive(rec.WorkerPID)
		if err == nil && !alive {
			repaired, repairErr := r.reconcileDeadWorker(taskID, rec)
			if repairErr != nil {
				return task.ResultPayload{}, repairErr
			}
			rec = repaired
		}
	}
	refreshedRec, touchErr := r.store.TouchActivity(rec)
	if touchErr != nil {
		r.logger.Warn("failed to update activity timestamp", "task_id", taskID, "error", touchErr.Error())
	} else {
		rec = refreshedRec
	}
	normalizedResult, _ := r.store.ReadNormalizedResult(taskID)
	result, _ := r.store.ReadResult(taskID)
	if normalizedResult != nil && normalizedResult.FinalText != "" {
		result = normalizedResult.FinalText
	}
	return payloadFromRecord(rec, result, normalizedResult), nil
}

func (r *Runner) reconcileDeadWorker(taskID string, rec task.Record) (task.Record, error) {
	for i := 0; i < statusRepairTries; i++ {
		latest, err := r.store.Load(taskID)
		if err != nil {
			return task.Record{}, err
		}
		if latest.Terminal() {
			return latest, nil
		}
		time.Sleep(statusRepairDelay)
	}

	latest, err := r.store.Load(taskID)
	if err != nil {
		return task.Record{}, err
	}
	if latest.Terminal() {
		return latest, nil
	}

	now := time.Now()
	rec.CompletedAt = &now
	if rec.Status == task.StatusRunning || rec.Status == task.StatusQueued {
		rec.Status = task.StatusFailed
		rec.Error = "worker exited unexpectedly before marking the task as completed"
		if saveErr := r.store.Save(rec); saveErr != nil {
			return task.Record{}, saveErr
		}
	}
	return rec, nil
}

// Wait blocks until a task reaches a final state or the timeout expires.
func (r *Runner) Wait(taskID string, timeoutSeconds int) (task.ResultPayload, error) {
	ctx := context.Background()
	if timeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		defer cancel()
	}
	return r.waitForTerminal(ctx, taskID, pollInterval)
}

func (r *Runner) waitForTerminal(ctx context.Context, taskID string, fallbackInterval time.Duration) (task.ResultPayload, error) {
	taskWatcher, err := r.openTaskWatcher(taskID)
	if err != nil {
		r.logger.Warn("failed to open task watcher; falling back to polling", "task_id", taskID, "error", err.Error())
	}
	if taskWatcher != nil {
		defer taskWatcher.Close()
	}
	for {
		status, err := r.Status(taskID)
		if err != nil {
			return task.ResultPayload{}, err
		}
		if status.Status == task.StatusSucceeded || status.Status == task.StatusFailed || status.Status == task.StatusCancelled {
			return status, nil
		}
		if err := r.waitForTaskChange(ctx, taskWatcher, fallbackInterval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return task.ResultPayload{}, fmt.Errorf("wait timeout for task %s", taskID)
			}
			return task.ResultPayload{}, err
		}
	}
}

// Cancel requests graceful shutdown for the worker and its supervised child process.
func (r *Runner) Cancel(taskID string) (task.ResultPayload, error) {
	rec, err := r.store.Load(taskID)
	if err != nil {
		return task.ResultPayload{}, err
	}
	if rec.Terminal() {
		normalizedResult, _ := r.store.ReadNormalizedResult(taskID)
		result, _ := r.store.ReadResult(taskID)
		if normalizedResult != nil && normalizedResult.FinalText != "" {
			result = normalizedResult.FinalText
		}
		return payloadFromRecord(rec, result, normalizedResult), nil
	}
	if rec.WorkerPID == 0 {
		return task.ResultPayload{}, errors.New("task has no worker pid")
	}
	proc, err := os.FindProcess(rec.WorkerPID)
	if err != nil {
		return task.ResultPayload{}, fmt.Errorf("find worker process: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return task.ResultPayload{}, fmt.Errorf("signal worker: %w", err)
	}
	deadline := time.Now().Add(cancelGracePeriod)
	for time.Now().Before(deadline) {
		alive, err := processAlive(rec.WorkerPID)
		if err == nil && !alive {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if alive, err := processAlive(rec.WorkerPID); err == nil && alive {
		_ = proc.Signal(syscall.SIGKILL)
	}
	now := time.Now()
	rec.Status = task.StatusCancelled
	rec.CompletedAt = &now
	cancelledExitCode := -1
	rec.ExitCode = &cancelledExitCode
	if rec.Error == "" {
		rec.Error = "task cancelled by user"
	}
	if err := r.store.Save(rec); err != nil {
		return task.ResultPayload{}, err
	}
	normalizedResult, _ := r.store.ReadNormalizedResult(taskID)
	result, _ := r.store.ReadResult(taskID)
	if normalizedResult != nil && normalizedResult.FinalText != "" {
		result = normalizedResult.FinalText
	}
	return payloadFromRecord(rec, result, normalizedResult), nil
}

// Logs returns the current stdout/stderr snapshot for a task.
func (r *Runner) Logs(taskID string) (task.LogsPayload, error) {
	return r.store.ReadLogs(taskID)
}

// SnapshotNormalizedResult returns the latest normalized result view for a task.
func (r *Runner) SnapshotNormalizedResult(taskID string) (*task.NormalizedResult, error) {
	rec, err := r.store.Load(taskID)
	if err != nil {
		return nil, err
	}
	if rec.Terminal() {
		if normalized, err := r.store.ReadNormalizedResult(taskID); err == nil && normalized != nil {
			return normalized, nil
		}
	}
	logs, err := r.store.ReadLogs(taskID)
	if err != nil {
		return nil, err
	}
	provider, err := providers.Registry(rec.Request.Provider)
	if err != nil {
		return nil, err
	}
	normalized := provider.ExtractResult(logs.Stdout, logs.Stderr, rec.Request)
	return &normalized, nil
}

// OpenTaskWatcher opens a task-directory watcher for event-driven status refresh.
func (r *Runner) OpenTaskWatcher(taskID string) (*store.TaskWatcher, error) {
	return r.openTaskWatcher(taskID)
}

// WaitForTaskChange blocks until the watcher fires or the fallback interval elapses.
func (r *Runner) WaitForTaskChange(ctx context.Context, taskWatcher *store.TaskWatcher, fallbackInterval time.Duration) error {
	return r.waitForTaskChange(ctx, taskWatcher, fallbackInterval)
}

// RunWorker executes the hidden worker command for a submitted task.
func (r *Runner) RunWorker(taskID string) (err error) {
	rec, err := r.store.Load(taskID)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		now := time.Now()
		rec.Status = task.StatusFailed
		rec.CompletedAt = &now
		rec.Error = err.Error()
		if saveErr := r.store.Save(rec); saveErr != nil {
			r.logger.Error("failed to persist worker error", "task_id", taskID, "error", saveErr.Error())
		}
	}()
	provider, err := providers.Registry(rec.Request.Provider)
	if err != nil {
		return err
	}
	if err := provider.Validate(rec.Request); err != nil {
		return err
	}
	cmdArgs, err := provider.BuildCommand(rec.Request)
	if err != nil {
		return err
	}
	rec.Command = cmdArgs
	rec.CommandString = strings.Join(cmdArgs, " ")
	rec.WorkerPID = os.Getpid()
	now := time.Now()
	rec.Status = task.StatusRunning
	rec.StartedAt = &now
	if err := r.store.Save(rec); err != nil {
		return err
	}

	stdoutFile, err := os.OpenFile(rec.StdoutPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open stdout file: %w", err)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.OpenFile(rec.StderrPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open stderr file: %w", err)
	}
	defer stderrFile.Close()

	ctx := context.Background()
	if rec.Request.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(rec.Request.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	childCmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	childCmd.Stdout = stdoutFile
	childCmd.Stderr = stderrFile
	childCmd.Stdin = nil
	childCmd.Dir = rec.Request.WorkingDir
	childCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	childCmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	if err := childCmd.Start(); err != nil {
		rec.Status = task.StatusFailed
		rec.Error = fmt.Sprintf("start provider: %v", err)
		completed := time.Now()
		rec.CompletedAt = &completed
		if saveErr := r.store.Save(rec); saveErr != nil {
			return saveErr
		}
		return err
	}
	rec.ChildPID = childCmd.Process.Pid
	if err := r.store.Save(rec); err != nil {
		return err
	}

	stopForward := make(chan struct{})
	defer close(stopForward)
	go forwardSignals(r.logger, childCmd.Process, stopForward)

	waitErr := childCmd.Wait()
	completedAt := time.Now()
	rec.CompletedAt = &completedAt
	latest, latestErr := latestActivity(rec.StdoutPath, rec.StderrPath)
	if latestErr == nil && !latest.IsZero() {
		rec.LastActivityAt = &latest
	}
	exitCode := exitCodeFromError(waitErr)
	rec.ExitCode = &exitCode

	stdoutBytes, err := os.ReadFile(rec.StdoutPath)
	if err != nil {
		return fmt.Errorf("read stdout for result extraction: %w", err)
	}
	stderrBytes, err := os.ReadFile(rec.StderrPath)
	if err != nil {
		return fmt.Errorf("read stderr for result extraction: %w", err)
	}
	normalizedResult := provider.ExtractResult(string(stdoutBytes), string(stderrBytes), rec.Request)
	if err := r.store.WriteResult(rec.TaskID, normalizedResult.FinalText); err != nil {
		return err
	}
	if err := r.store.WriteNormalizedResult(rec.TaskID, normalizedResult); err != nil {
		return err
	}

	switch {
	case errors.Is(waitErr, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		rec.Status = task.StatusFailed
		rec.Error = "task timed out"
	case exitCode == -1:
		rec.Status = task.StatusCancelled
		rec.Error = "task cancelled"
	case waitErr != nil:
		rec.Status = task.StatusFailed
		rec.Error = waitErr.Error()
	default:
		rec.Status = task.StatusSucceeded
		rec.Error = ""
	}
	if err := r.store.Save(rec); err != nil {
		return err
	}
	err = nil
	return nil
}

func (r *Runner) openTaskWatcher(taskID string) (*store.TaskWatcher, error) {
	return r.store.WatchTask(taskID)
}

func (r *Runner) waitForTaskChange(ctx context.Context, taskWatcher *store.TaskWatcher, fallbackInterval time.Duration) error {
	if taskWatcher != nil {
		return taskWatcher.Wait(ctx, fallbackInterval)
	}
	timer := time.NewTimer(fallbackInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func payloadFromRecord(rec task.Record, result string, normalizedResult *task.NormalizedResult) task.ResultPayload {
	return task.ResultPayload{
		TaskID:               rec.TaskID,
		Provider:             rec.Request.Provider,
		Status:               rec.Status,
		WorkerPID:            rec.WorkerPID,
		ChildPID:             rec.ChildPID,
		ExitCode:             rec.ExitCode,
		Command:              rec.CommandString,
		Result:               result,
		NormalizedResult:     normalizedResult,
		Error:                rec.Error,
		StartedAt:            rec.StartedAt,
		CompletedAt:          rec.CompletedAt,
		LastActivityAt:       rec.LastActivityAt,
		StdoutPath:           rec.StdoutPath,
		StderrPath:           rec.StderrPath,
		ResultPath:           rec.ResultPath,
		NormalizedResultPath: rec.NormalizedResultPath,
	}
}

func newTaskID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func processAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	if err := syscall.Kill(pid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, err
	}
	psCmd := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "stat=")
	output, err := psCmd.Output()
	if err != nil {
		return false, nil
	}
	state := strings.TrimSpace(string(output))
	return state != "" && !strings.HasPrefix(state, "Z"), nil
}

func latestActivity(paths ...string) (time.Time, error) {
	var latest time.Time
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return time.Time{}, err
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest, nil
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if errors.Is(err, context.Canceled) {
		return -1
	}
	return 1
}

// filterEnv returns a copy of environ with entries matching any of the given
// variable names removed. This prevents parent-session markers (e.g. CLAUDECODE)
// from leaking into child provider processes.
func filterEnv(environ []string, names ...string) []string {
	prefixes := make([]string, len(names))
	for i, name := range names {
		prefixes[i] = name + "="
	}
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func forwardSignals(logger *slog.Logger, child *os.Process, done <-chan struct{}) {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	for {
		select {
		case <-done:
			return
		case sig := <-signals:
			if child == nil {
				return
			}
			if err := child.Signal(sig); err != nil {
				logger.Warn("failed to forward signal", "signal", sig.String(), "error", err.Error())
			}
		}
	}
}
