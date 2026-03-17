package providers

import (
	"strings"
	"testing"

	"async-agent-backend/internal/task"
)

func TestClaudeBuildCommandUsesHeadlessDefaults(t *testing.T) {
	t.Parallel()

	provider, err := Registry("claude")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	cmd, err := provider.BuildCommand(task.Request{
		Provider: "claude",
		Task:     "inspect repo",
		Model:    "sonnet",
		Args:     []string{"--allowedTools", "Read"},
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	joined := strings.Join(cmd, " ")
	for _, token := range []string{"claude", "--print", "inspect repo", "--output-format", "stream-json", "--verbose", "--model", "sonnet", "--dangerously-skip-permissions"} {
		if !strings.Contains(joined, token) {
			t.Fatalf("expected %q in command %q", token, joined)
		}
	}
}

func TestCodexBuildCommandAddsRepoBypass(t *testing.T) {
	t.Parallel()

	provider, err := Registry("codex")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	cmd, err := provider.BuildCommand(task.Request{
		Provider:   "codex",
		Task:       "fix bug",
		Subcommand: "exec",
		Args:       []string{"--full-auto"},
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	joined := strings.Join(cmd, " ")
	if !strings.Contains(joined, "--skip-git-repo-check") {
		t.Fatalf("expected codex command to contain --skip-git-repo-check, got %q", joined)
	}
}

func TestCodexResumeCommandPlacesSessionBeforeFollowUpPrompt(t *testing.T) {
	t.Parallel()

	provider, err := Registry("codex")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	cmd, err := provider.BuildCommand(task.Request{
		Provider:      "codex",
		Subcommand:    "resume",
		ResumeSession: "session-42",
		Task:          "finish the review",
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	joined := strings.Join(cmd, " ")
	if !strings.Contains(joined, "resume session-42 finish the review") {
		t.Fatalf("expected resume command to include session then prompt, got %q", joined)
	}
}

func TestGeminiExtractsStreamJSON(t *testing.T) {
	t.Parallel()

	provider, err := Registry("gemini")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	result := provider.ExtractResult(
		"{\"text\":\"part one\"}\n{\"result\":\"part two\"}\n",
		"",
		task.Request{Provider: "gemini", OutputFormat: "stream-json"},
	)
	if result.OutputMode != "stream-json" || result.FinalText != "part one\npart two" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected normalized events, got %+v", result)
	}
	if result.Events[0].EventType != "message" || result.Events[0].Text != "part one" {
		t.Fatalf("unexpected first event: %+v", result.Events[0])
	}
	if result.Events[1].EventType != "result" || !result.Events[1].IsTerminal {
		t.Fatalf("unexpected second event: %+v", result.Events[1])
	}
}

func TestClaudeExtractsResultObject(t *testing.T) {
	t.Parallel()

	provider, err := Registry("claude")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	result := provider.ExtractResult(
		"{\"type\":\"meta\"}\n{\"type\":\"result\",\"result\":\"done\"}\n",
		"",
		task.Request{Provider: "claude", OutputFormat: "stream-json"},
	)
	if result.OutputMode != "stream-json" || result.FinalText != "done" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected normalized events, got %+v", result)
	}
	if result.Events[0].EventType != "meta" || result.Events[0].RawType != "meta" {
		t.Fatalf("unexpected first event: %+v", result.Events[0])
	}
	if result.Events[1].EventType != "result" || result.Events[1].Text != "done" || !result.Events[1].IsTerminal {
		t.Fatalf("unexpected second event: %+v", result.Events[1])
	}
}

func TestCodexJSONNormalizesToolAndResultEvents(t *testing.T) {
	t.Parallel()

	provider, err := Registry("codex")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	result := provider.ExtractResult(
		"{\"type\":\"tool_use\",\"name\":\"search\",\"input\":\"q\"}\n{\"type\":\"result\",\"result\":\"done\"}\n",
		"",
		task.Request{Provider: "codex", Args: []string{"--json"}},
	)
	if result.OutputMode != "stream-json" || result.FinalText != "done" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected normalized events, got %+v", result)
	}
	if result.Events[0].EventType != "tool_use" || result.Events[0].ToolName != "search" {
		t.Fatalf("unexpected tool event: %+v", result.Events[0])
	}
	if result.Events[1].EventType != "result" || !result.Events[1].IsTerminal {
		t.Fatalf("unexpected result event: %+v", result.Events[1])
	}
}

func TestGeminiBuildCommandUsesHeadlessPromptAndExpandedFlags(t *testing.T) {
	t.Parallel()

	provider, err := Registry("gemini")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	cmd, err := provider.BuildCommand(task.Request{
		Provider:              "gemini",
		Task:                  "inspect logs",
		OutputFormat:          "json",
		ApprovalMode:          "auto_edit",
		Sandbox:               true,
		ResumeSession:         "latest",
		IncludeDirectories:    []string{"/tmp/a", "/tmp/b"},
		AllowedMCPServerNames: []string{"filesystem"},
		Extensions:            []string{"foo"},
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	joined := strings.Join(cmd, " ")
	for _, expected := range []string{
		"gemini",
		"--output-format json",
		"--approval-mode auto_edit",
		"--sandbox",
		"--resume latest",
		"--include-directories /tmp/a",
		"--include-directories /tmp/b",
		"--allowed-mcp-server-names filesystem",
		"--extensions foo",
		"--prompt inspect logs",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in command %q", expected, joined)
		}
	}
}

func TestClaudeBuildCommandIncludesBudgetAndPermissionPromptTool(t *testing.T) {
	t.Parallel()

	provider, err := Registry("claude")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	cmd, err := provider.BuildCommand(task.Request{
		Provider:             "claude",
		Task:                 "budgeted run",
		MaxBudgetUSD:         1.25,
		PermissionPromptTool: "mcp__perm__prompt",
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	joined := strings.Join(cmd, " ")
	for _, expected := range []string{
		"--max-budget-usd 1.25",
		"--permission-prompt-tool mcp__perm__prompt",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in command %q", expected, joined)
		}
	}
}

func TestCodexBuildCommandIncludesAutomationFlags(t *testing.T) {
	t.Parallel()

	provider, err := Registry("codex")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	cmd, err := provider.BuildCommand(task.Request{
		Provider:        "codex",
		Subcommand:      "exec",
		Task:            "automation",
		FullAuto:        true,
		Images:          []string{"/tmp/a.png", "/tmp/b.png"},
		Profile:         "ci-profile",
		ConfigOverrides: []string{"foo=bar"},
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	joined := strings.Join(cmd, " ")
	for _, expected := range []string{
		"--full-auto",
		"--image /tmp/a.png,/tmp/b.png",
		"--profile ci-profile",
		"--config foo=bar",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in command %q", expected, joined)
		}
	}
}

func TestGeminiValidateAllowsSessionManagementWithoutPrompt(t *testing.T) {
	t.Parallel()

	provider, err := Registry("gemini")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	if err := provider.Validate(task.Request{
		Provider:      "gemini",
		ListSessions:  true,
		DeleteSession: "latest",
	}); err != nil {
		t.Fatalf("expected session management request to validate, got %v", err)
	}
}
