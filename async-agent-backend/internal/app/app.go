package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"async-agent-backend/internal/mcp"
	"async-agent-backend/internal/runner"
	"async-agent-backend/internal/store"
	"async-agent-backend/internal/task"
)

// App wires the CLI, worker mode and MCP server to the backend runner.
type App struct {
	logger *slog.Logger
}

// New creates an application instance backed by slog.
func New(logger *slog.Logger) *App {
	return &App{logger: logger}
}

// Run executes the requested subcommand.
func (a *App) Run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		printHelp(stdout)
		return nil
	}

	switch args[0] {
	case "submit":
		return a.handleSubmit(args[1:], stdout)
	case "run":
		return a.handleRun(args[1:], stdout)
	case "status":
		return a.handleStatus(args[1:], stdout)
	case "wait":
		return a.handleWait(args[1:], stdout)
	case "logs":
		return a.handleLogs(args[1:], stdout)
	case "cancel":
		return a.handleCancel(args[1:], stdout)
	case "mcp":
		return a.handleMCP(args[1:], stdout)
	case "worker":
		return a.handleWorker(args[1:])
	case "help", "--help", "-h":
		printHelp(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a *App) handleSubmit(args []string, stdout io.Writer) error {
	req, _, taskRunner, err := a.parseExecutionCommand("submit", args)
	if err != nil {
		return err
	}
	rec, err := taskRunner.Submit(req)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]any{
		"task_id":     rec.TaskID,
		"provider":    rec.Request.Provider,
		"status":      rec.Status,
		"worker_pid":  rec.WorkerPID,
		"stdout_path": rec.StdoutPath,
		"stderr_path": rec.StderrPath,
		"result_path": rec.ResultPath,
	})
}

func (a *App) handleRun(args []string, stdout io.Writer) error {
	req, _, taskRunner, err := a.parseExecutionCommand("run", args)
	if err != nil {
		return err
	}
	result, err := taskRunner.Run(req)
	if err != nil {
		return err
	}
	return writeJSON(stdout, renderResultPayload(result, req.ResultMode))
}

func (a *App) handleStatus(args []string, stdout io.Writer) error {
	taskID, resultMode, storeRoot, taskRunner, err := a.parseTaskCommand("status", args)
	if err != nil {
		return err
	}
	_ = storeRoot
	result, err := taskRunner.Status(taskID)
	if err != nil {
		return err
	}
	return writeJSON(stdout, renderResultPayload(result, resultMode))
}

func (a *App) handleWait(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("wait", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	storeRoot := flags.String("store-root", "", "Task store root")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Wait timeout in seconds")
	resultMode := flags.String("result-mode", "", "Result rendering mode")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("wait requires a single task id argument")
	}
	taskRunner, err := a.newRunner(*storeRoot)
	if err != nil {
		return err
	}
	result, err := taskRunner.Wait(flags.Arg(0), *timeoutSeconds)
	if err != nil {
		return err
	}
	return writeJSON(stdout, renderResultPayload(result, *resultMode))
}

func (a *App) handleLogs(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	storeRoot := flags.String("store-root", "", "Task store root")
	follow := flags.Bool("follow", false, "Stream appended output as JSONL events")
	pollMillis := flags.Int("poll-ms", 500, "Polling interval for --follow")
	eventMode := flags.String("event-mode", "raw", "Follow output mode: raw or normalized")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("logs requires a single task id argument")
	}
	taskRunner, err := a.newRunner(*storeRoot)
	if err != nil {
		return err
	}
	taskID := flags.Arg(0)
	if !*follow {
		logs, err := taskRunner.Logs(taskID)
		if err != nil {
			return err
		}
		return writeJSON(stdout, logs)
	}
	if *eventMode == "normalized" {
		return a.followNormalizedEvents(taskRunner, taskID, stdout, time.Duration(*pollMillis)*time.Millisecond)
	}
	return a.followLogs(taskRunner, taskID, stdout, time.Duration(*pollMillis)*time.Millisecond)
}

func (a *App) handleCancel(args []string, stdout io.Writer) error {
	taskID, _, _, taskRunner, err := a.parseTaskCommand("cancel", args)
	if err != nil {
		return err
	}
	result, err := taskRunner.Cancel(taskID)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func (a *App) handleMCP(args []string, _ io.Writer) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	storeRoot := flags.String("store-root", "", "Task store root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	taskRunner, err := a.newRunner(*storeRoot)
	if err != nil {
		return err
	}
	return mcp.Serve(taskRunner, os.Stdin, os.Stdout)
}

func (a *App) handleWorker(args []string) error {
	flags := flag.NewFlagSet("worker", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	storeRoot := flags.String("store-root", "", "Task store root")
	taskID := flags.String("task-id", "", "Task identifier")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *taskID == "" {
		return errors.New("worker requires --task-id")
	}
	taskRunner, err := a.newRunner(*storeRoot)
	if err != nil {
		return err
	}
	return taskRunner.RunWorker(*taskID)
}

func (a *App) parseExecutionCommand(name string, args []string) (task.Request, string, *runner.Runner, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	provider := flags.String("provider", "", "Provider name: claude, codex or gemini")
	taskText := flags.String("task", "", "Task/prompt/query text")
	workingDir := flags.String("working-dir", "", "Working directory")
	model := flags.String("model", "", "Provider model")
	outputFormat := flags.String("output-format", "", "Provider output format")
	resultMode := flags.String("result-mode", "", "Result rendering mode")
	subcommand := flags.String("subcommand", "", "Provider subcommand")
	storeRoot := flags.String("store-root", "", "Task store root")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Provider timeout in seconds")
	skipPermissions := flags.Bool("skip-permissions", true, "Claude skip permissions")
	jsonSchema := flags.String("json-schema", "", "Claude JSON schema")
	continueSession := flags.Bool("continue", false, "Continue the latest provider session")
	resumeSession := flags.String("resume", "", "Resume a provider session")
	maxTurns := flags.Int("max-turns", 0, "Claude max turns")
	toolsMode := flags.String("tools-mode", "", "Claude tools mode")
	permissionMode := flags.String("permission-mode", "", "Claude permission mode")
	maxBudgetUSD := flags.Float64("max-budget-usd", 0, "Claude max budget in USD")
	permissionPromptTool := flags.String("permission-prompt-tool", "", "Claude permission prompt MCP tool")
	approvalMode := flags.String("approval-mode", "", "Approval mode")
	sandboxMode := flags.String("sandbox-mode", "", "Sandbox mode")
	sandbox := flags.Bool("sandbox", false, "Enable sandbox")
	ephemeral := flags.Bool("ephemeral", false, "Disable provider session persistence")
	search := flags.Bool("search", false, "Enable provider web search")
	resumeLast := flags.Bool("resume-last", false, "Use the latest resumable codex session")
	resumeAll := flags.Bool("resume-all", false, "List all resumable codex sessions")
	fullAuto := flags.Bool("full-auto", false, "Enable Codex full-auto mode")
	debug := flags.Bool("debug", false, "Enable provider debug output")
	experimentalACP := flags.Bool("experimental-acp", false, "Enable Gemini experimental ACP")
	screenReader := flags.Bool("screen-reader", false, "Enable Gemini screen-reader mode")
	listSessions := flags.Bool("list-sessions", false, "List Gemini sessions")
	deleteSession := flags.String("delete-session", "", "Delete a Gemini session")
	outputLastMessagePath := flags.String("output-last-message", "", "Write the final provider message to a file")
	outputSchemaPath := flags.String("output-schema", "", "Path to a JSON schema file for structured output")
	var addDirs multiString
	var allowedTools multiString
	var disallowedTools multiString
	var images multiString
	var configOverrides multiString
	var includeDirectories multiString
	var allowedMCPServerNames multiString
	var extensions multiString
	flags.Var(&addDirs, "add-dir", "Additional provider-accessible directory")
	flags.Var(&allowedTools, "allowed-tool", "Allowed provider tool")
	flags.Var(&disallowedTools, "disallowed-tool", "Disallowed Claude tool")
	flags.Var(&images, "image", "Codex image path")
	flags.Var(&configOverrides, "config-override", "Codex config override")
	flags.Var(&includeDirectories, "include-directory", "Additional Gemini include directory")
	flags.Var(&allowedMCPServerNames, "allowed-mcp-server-name", "Allowed Gemini MCP server")
	flags.Var(&extensions, "extension", "Enabled Gemini extension")
	profile := flags.String("profile", "", "Codex profile")
	var extraArgs multiString
	flags.Var(&extraArgs, "arg", "Additional provider argument")
	if err := flags.Parse(args); err != nil {
		return task.Request{}, "", nil, err
	}
	req := task.Request{
		Provider:              *provider,
		ResultMode:            *resultMode,
		Task:                  *taskText,
		WorkingDir:            *workingDir,
		Model:                 *model,
		OutputFormat:          *outputFormat,
		Subcommand:            *subcommand,
		Args:                  extraArgs,
		TimeoutSeconds:        *timeoutSeconds,
		JSONSchema:            *jsonSchema,
		ContinueSession:       *continueSession,
		ResumeSession:         *resumeSession,
		MaxTurns:              *maxTurns,
		AddDirs:               addDirs,
		AllowedTools:          allowedTools,
		DisallowedTools:       disallowedTools,
		ToolsMode:             *toolsMode,
		PermissionMode:        *permissionMode,
		MaxBudgetUSD:          *maxBudgetUSD,
		PermissionPromptTool:  *permissionPromptTool,
		ApprovalMode:          *approvalMode,
		SandboxMode:           *sandboxMode,
		Sandbox:               *sandbox,
		Ephemeral:             *ephemeral,
		Search:                *search,
		ResumeLast:            *resumeLast,
		ResumeAll:             *resumeAll,
		FullAuto:              *fullAuto,
		Images:                images,
		Profile:               *profile,
		ConfigOverrides:       configOverrides,
		OutputLastMessagePath: *outputLastMessagePath,
		OutputSchemaPath:      *outputSchemaPath,
		IncludeDirectories:    includeDirectories,
		AllowedMCPServerNames: allowedMCPServerNames,
		Extensions:            extensions,
		Debug:                 *debug,
		ExperimentalACP:       *experimentalACP,
		ScreenReader:          *screenReader,
		ListSessions:          *listSessions,
		DeleteSession:         *deleteSession,
	}
	req.SkipPermissions = skipPermissions
	taskRunner, err := a.newRunner(*storeRoot)
	if err != nil {
		return task.Request{}, "", nil, err
	}
	return req, *storeRoot, taskRunner, nil
}

func (a *App) parseTaskCommand(name string, args []string) (string, string, string, *runner.Runner, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	storeRoot := flags.String("store-root", "", "Task store root")
	resultMode := flags.String("result-mode", "", "Result rendering mode")
	if err := flags.Parse(args); err != nil {
		return "", "", "", nil, err
	}
	if flags.NArg() != 1 {
		return "", "", "", nil, fmt.Errorf("%s requires a single task id argument", name)
	}
	taskRunner, err := a.newRunner(*storeRoot)
	if err != nil {
		return "", "", "", nil, err
	}
	return flags.Arg(0), *resultMode, *storeRoot, taskRunner, nil
}

func (a *App) newRunner(storeRoot string) (*runner.Runner, error) {
	taskStore, err := store.New(storeRoot)
	if err != nil {
		return nil, err
	}
	return runner.New(taskStore, "", a.logger)
}

func (a *App) followLogs(taskRunner *runner.Runner, taskID string, stdout io.Writer, pollInterval time.Duration) error {
	taskWatcher, err := taskRunner.OpenTaskWatcher(taskID)
	if err != nil {
		a.logger.Warn("failed to open task watcher; falling back to polling", "task_id", taskID, "error", err.Error())
	}
	if taskWatcher != nil {
		defer taskWatcher.Close()
	}
	var lastStdoutLen int
	var lastStderrLen int
	for {
		logs, err := taskRunner.Logs(taskID)
		if err != nil {
			return err
		}
		if len(logs.Stdout) > lastStdoutLen {
			if err := writeJSONLine(stdout, map[string]any{"stream": "stdout", "chunk": logs.Stdout[lastStdoutLen:]}); err != nil {
				return err
			}
			lastStdoutLen = len(logs.Stdout)
		}
		if len(logs.Stderr) > lastStderrLen {
			if err := writeJSONLine(stdout, map[string]any{"stream": "stderr", "chunk": logs.Stderr[lastStderrLen:]}); err != nil {
				return err
			}
			lastStderrLen = len(logs.Stderr)
		}
		status, err := taskRunner.Status(taskID)
		if err != nil {
			return err
		}
		if status.Status == task.StatusSucceeded || status.Status == task.StatusFailed || status.Status == task.StatusCancelled {
			logs, err := taskRunner.Logs(taskID)
			if err != nil {
				return err
			}
			if len(logs.Stdout) > lastStdoutLen {
				if err := writeJSONLine(stdout, map[string]any{"stream": "stdout", "chunk": logs.Stdout[lastStdoutLen:]}); err != nil {
					return err
				}
			}
			if len(logs.Stderr) > lastStderrLen {
				if err := writeJSONLine(stdout, map[string]any{"stream": "stderr", "chunk": logs.Stderr[lastStderrLen:]}); err != nil {
					return err
				}
			}
			return writeJSONLine(stdout, map[string]any{"stream": "status", "status": status.Status})
		}
		if err := taskRunner.WaitForTaskChange(context.Background(), taskWatcher, pollInterval); err != nil {
			return err
		}
	}
}

func (a *App) followNormalizedEvents(taskRunner *runner.Runner, taskID string, stdout io.Writer, pollInterval time.Duration) error {
	taskWatcher, err := taskRunner.OpenTaskWatcher(taskID)
	if err != nil {
		a.logger.Warn("failed to open task watcher; falling back to polling", "task_id", taskID, "error", err.Error())
	}
	if taskWatcher != nil {
		defer taskWatcher.Close()
	}
	lastEventIndex := 0
	for {
		normalized, err := taskRunner.SnapshotNormalizedResult(taskID)
		if err != nil {
			return err
		}
		if normalized != nil && len(normalized.Events) > lastEventIndex {
			for _, event := range normalized.Events[lastEventIndex:] {
				if err := writeJSONLine(stdout, map[string]any{"stream": "event", "event": event}); err != nil {
					return err
				}
			}
			lastEventIndex = len(normalized.Events)
		}
		status, err := taskRunner.Status(taskID)
		if err != nil {
			return err
		}
		if status.Status == task.StatusSucceeded || status.Status == task.StatusFailed || status.Status == task.StatusCancelled {
			normalized, err := taskRunner.SnapshotNormalizedResult(taskID)
			if err != nil {
				return err
			}
			if normalized != nil && len(normalized.Events) > lastEventIndex {
				for _, event := range normalized.Events[lastEventIndex:] {
					if err := writeJSONLine(stdout, map[string]any{"stream": "event", "event": event}); err != nil {
						return err
					}
				}
			}
			return writeJSONLine(stdout, map[string]any{"stream": "status", "status": status.Status})
		}
		if err := taskRunner.WaitForTaskChange(context.Background(), taskWatcher, pollInterval); err != nil {
			return err
		}
	}
}

func writeJSON(writer io.Writer, payload any) error {
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(writer, string(bytes))
	return err
}

func writeJSONLine(writer io.Writer, payload any) error {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(writer, string(bytes))
	return err
}

func printHelp(writer io.Writer) {
	_, _ = fmt.Fprint(writer, `async-agent-backend

Commands:
  submit --provider <name> --task <text> [--working-dir dir] [--arg value...]
  run --provider <name> --task <text> [--working-dir dir] [--arg value...]
  status [--store-root dir] <task-id>
  wait [--store-root dir] [--timeout-seconds n] <task-id>
  logs [--store-root dir] [--follow] <task-id>
  cancel [--store-root dir] <task-id>
  mcp [--store-root dir]
`)
}

type multiString []string

func (m *multiString) String() string {
	return fmt.Sprintf("%v", []string(*m))
}

func (m *multiString) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func renderResultPayload(payload task.ResultPayload, mode string) any {
	if mode != "normalized" {
		return payload
	}
	return map[string]any{
		"task_id":                payload.TaskID,
		"provider":               payload.Provider,
		"status":                 payload.Status,
		"exit_code":              payload.ExitCode,
		"error":                  payload.Error,
		"normalized_result":      payload.NormalizedResult,
		"normalized_result_path": payload.NormalizedResultPath,
	}
}
