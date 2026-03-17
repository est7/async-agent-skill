package task

import "time"

// Status describes the lifecycle state of a submitted task.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Request captures a normalized provider execution request.
type Request struct {
	Provider              string   `json:"provider"`
	ResultMode            string   `json:"result_mode,omitempty"`
	Task                  string   `json:"task"`
	WorkingDir            string   `json:"working_dir,omitempty"`
	Model                 string   `json:"model,omitempty"`
	OutputFormat          string   `json:"output_format,omitempty"`
	Subcommand            string   `json:"subcommand,omitempty"`
	Args                  []string `json:"args,omitempty"`
	TimeoutSeconds        int      `json:"timeout_seconds,omitempty"`
	SkipPermissions       *bool    `json:"skip_permissions,omitempty"`
	JSONSchema            string   `json:"json_schema,omitempty"`
	ContinueSession       bool     `json:"continue_session,omitempty"`
	ResumeSession         string   `json:"resume_session,omitempty"`
	MaxTurns              int      `json:"max_turns,omitempty"`
	AddDirs               []string `json:"add_dirs,omitempty"`
	AllowedTools          []string `json:"allowed_tools,omitempty"`
	DisallowedTools       []string `json:"disallowed_tools,omitempty"`
	ToolsMode             string   `json:"tools_mode,omitempty"`
	PermissionMode        string   `json:"permission_mode,omitempty"`
	MaxBudgetUSD          float64  `json:"max_budget_usd,omitempty"`
	PermissionPromptTool  string   `json:"permission_prompt_tool,omitempty"`
	ApprovalMode          string   `json:"approval_mode,omitempty"`
	SandboxMode           string   `json:"sandbox_mode,omitempty"`
	Sandbox               bool     `json:"sandbox,omitempty"`
	Ephemeral             bool     `json:"ephemeral,omitempty"`
	Search                bool     `json:"search,omitempty"`
	ResumeLast            bool     `json:"resume_last,omitempty"`
	ResumeAll             bool     `json:"resume_all,omitempty"`
	FullAuto              bool     `json:"full_auto,omitempty"`
	Images                []string `json:"images,omitempty"`
	Profile               string   `json:"profile,omitempty"`
	ConfigOverrides       []string `json:"config_overrides,omitempty"`
	OutputLastMessagePath string   `json:"output_last_message_path,omitempty"`
	OutputSchemaPath      string   `json:"output_schema_path,omitempty"`
	IncludeDirectories    []string `json:"include_directories,omitempty"`
	AllowedMCPServerNames []string `json:"allowed_mcp_server_names,omitempty"`
	Extensions            []string `json:"extensions,omitempty"`
	Debug                 bool     `json:"debug,omitempty"`
	ExperimentalACP       bool     `json:"experimental_acp,omitempty"`
	ScreenReader          bool     `json:"screen_reader,omitempty"`
	ListSessions          bool     `json:"list_sessions,omitempty"`
	DeleteSession         string   `json:"delete_session,omitempty"`
}

// Record stores persisted task metadata.
type Record struct {
	TaskID               string     `json:"task_id"`
	Request              Request    `json:"request"`
	Status               Status     `json:"status"`
	Command              []string   `json:"command,omitempty"`
	CommandString        string     `json:"command_string,omitempty"`
	WorkerPID            int        `json:"worker_pid,omitempty"`
	ChildPID             int        `json:"child_pid,omitempty"`
	ExitCode             *int       `json:"exit_code,omitempty"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	LastActivityAt       *time.Time `json:"last_activity_at,omitempty"`
	StdoutPath           string     `json:"stdout_path"`
	StderrPath           string     `json:"stderr_path"`
	ResultPath           string     `json:"result_path"`
	NormalizedResultPath string     `json:"normalized_result_path"`
	Error                string     `json:"error,omitempty"`
}

// Terminal reports whether the task already reached a final state.
func (r Record) Terminal() bool {
	return r.Status == StatusSucceeded || r.Status == StatusFailed || r.Status == StatusCancelled
}

// NormalizedResult provides a unified result envelope across provider output modes.
type NormalizedEvent struct {
	EventType  string `json:"event_type"`
	RawType    string `json:"raw_type,omitempty"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	IsTerminal bool   `json:"is_terminal,omitempty"`
}

type NormalizedResult struct {
	OutputMode       string            `json:"output_mode"`
	FinalText        string            `json:"final_text,omitempty"`
	StructuredOutput any               `json:"structured_output,omitempty"`
	StreamEventCount int               `json:"stream_event_count,omitempty"`
	StreamEventTypes []string          `json:"stream_event_types,omitempty"`
	Events           []NormalizedEvent `json:"events,omitempty"`
}

// ResultPayload is returned by CLI and MCP commands.
type ResultPayload struct {
	TaskID               string            `json:"task_id"`
	Provider             string            `json:"provider"`
	Status               Status            `json:"status"`
	WorkerPID            int               `json:"worker_pid,omitempty"`
	ChildPID             int               `json:"child_pid,omitempty"`
	ExitCode             *int              `json:"exit_code,omitempty"`
	Command              string            `json:"command,omitempty"`
	Result               string            `json:"result,omitempty"`
	NormalizedResult     *NormalizedResult `json:"normalized_result,omitempty"`
	Error                string            `json:"error,omitempty"`
	StartedAt            *time.Time        `json:"started_at,omitempty"`
	CompletedAt          *time.Time        `json:"completed_at,omitempty"`
	LastActivityAt       *time.Time        `json:"last_activity_at,omitempty"`
	StdoutPath           string            `json:"stdout_path,omitempty"`
	StderrPath           string            `json:"stderr_path,omitempty"`
	ResultPath           string            `json:"result_path,omitempty"`
	NormalizedResultPath string            `json:"normalized_result_path,omitempty"`
}

// LogsPayload captures stored output streams.
type LogsPayload struct {
	TaskID string `json:"task_id"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}
