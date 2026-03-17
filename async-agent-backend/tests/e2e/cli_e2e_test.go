package e2e_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLI_SubmitStatusWaitLogs(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	claudePath := writeScript(t, filepath.Dir(binaryPath), "fake-claude.sh", "#!/bin/sh\nsleep 1\necho '{\"type\":\"result\",\"result\":\"fake claude output\"}'\n")
	env = append(env, "ASYNC_AGENT_CLAUDE_BIN="+claudePath)

	submitOutput := runCommand(t, env, binaryPath, "submit", "--store-root", storeRoot, "--provider", "claude", "--task", "analyze")
	submitPayload := parseJSONMap(t, submitOutput)
	taskID := mustTaskID(t, submitPayload, submitOutput)

	statusOutput := runCommand(t, env, binaryPath, "status", "--store-root", storeRoot, taskID)
	statusPayload := parseJSONMap(t, statusOutput)
	if statusPayload["status"] == "" {
		t.Fatalf("expected non-empty status payload: %s", statusOutput)
	}

	waitOutput := runCommand(t, env, binaryPath, "wait", "--store-root", storeRoot, "--timeout-seconds", "10", taskID)
	waitPayload := parseJSONMap(t, waitOutput)
	if waitPayload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", waitPayload["status"], waitOutput)
	}
	if waitPayload["result"] != "fake claude output" {
		t.Fatalf("unexpected result payload: %s", waitOutput)
	}

	logsOutput := runCommand(t, env, binaryPath, "logs", "--store-root", storeRoot, taskID)
	logsPayload := parseJSONMap(t, logsOutput)
	stdoutValue, _ := logsPayload["stdout"].(string)
	if stdoutValue == "" {
		t.Fatalf("expected stdout logs to be persisted: %s", logsOutput)
	}
}

func TestCLI_FollowLogsStreamsAndFinishes(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	geminiPath := writeScript(t, filepath.Dir(binaryPath), "fake-gemini.sh", "#!/bin/sh\necho 'hello'\nsleep 1\necho 'world'\n")
	env = append(env, "ASYNC_AGENT_GEMINI_BIN="+geminiPath)

	submitOutput := runCommand(t, env, binaryPath, "submit", "--store-root", storeRoot, "--provider", "gemini", "--task", "analyze")
	taskID := mustTaskID(t, parseJSONMap(t, submitOutput), submitOutput)

	cmd := exec.Command(binaryPath, "logs", "--store-root", storeRoot, "--follow", "--poll-ms", "100", taskID)
	cmd.Env = env
	outputCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		output, err := cmd.CombinedOutput()
		outputCh <- output
		errCh <- err
	}()

	select {
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("timed out waiting for follow logs")
	case output := <-outputCh:
		if err := <-errCh; err != nil {
			t.Fatalf("follow logs command failed: %v\n%s", err, output)
		}
		lines := nonEmptyLines(output)
		if len(lines) < 3 {
			t.Fatalf("expected follow log lines, got %s", output)
		}
		first := parseJSONMap(t, []byte(lines[0]))
		second := parseJSONMap(t, []byte(lines[1]))
		last := parseJSONMap(t, []byte(lines[len(lines)-1]))
		if first["chunk"] != "hello\n" || first["stream"] != "stdout" {
			t.Fatalf("unexpected first follow chunk: %s", lines[0])
		}
		if second["chunk"] != "world\n" || second["stream"] != "stdout" {
			t.Fatalf("unexpected second follow chunk: %s", lines[1])
		}
		if last["status"] != "succeeded" || last["stream"] != "status" {
			t.Fatalf("unexpected terminal status event: %s", lines[len(lines)-1])
		}
	}
}

func TestCLI_FollowLogsStreamsNormalizedEvents(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	geminiPath := writeScript(t, filepath.Dir(binaryPath), "fake-gemini-normalized-follow.sh", "#!/bin/sh\necho '{\"type\":\"message\",\"text\":\"hello\"}'\nsleep 1\necho '{\"type\":\"result\",\"result\":\"done\"}'\n")
	env = append(env, "ASYNC_AGENT_GEMINI_BIN="+geminiPath)

	submitOutput := runCommand(t, env, binaryPath, "submit", "--store-root", storeRoot, "--provider", "gemini", "--task", "analyze", "--output-format", "stream-json")
	taskID := mustTaskID(t, parseJSONMap(t, submitOutput), submitOutput)

	cmd := exec.Command(binaryPath, "logs", "--store-root", storeRoot, "--follow", "--poll-ms", "100", "--event-mode", "normalized", taskID)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("follow normalized logs command failed: %v\n%s", err, output)
	}

	lines := nonEmptyLines(output)
	if len(lines) < 3 {
		t.Fatalf("expected event and status lines, got %s", output)
	}
	first := parseJSONMap(t, []byte(lines[0]))
	if first["stream"] != "event" {
		t.Fatalf("expected first normalized envelope to be event, got %s", lines[0])
	}
	event := mustNestedMap(t, first, "event")
	if event["event_type"] != "message" || event["text"] != "hello" {
		t.Fatalf("unexpected first normalized event: %s", lines[0])
	}
	second := parseJSONMap(t, []byte(lines[1]))
	event = mustNestedMap(t, second, "event")
	if event["event_type"] != "result" || event["is_terminal"] != true {
		t.Fatalf("unexpected second normalized event: %s", lines[1])
	}
	last := parseJSONMap(t, []byte(lines[len(lines)-1]))
	if last["stream"] != "status" || last["status"] != "succeeded" {
		t.Fatalf("unexpected final status envelope: %s", lines[len(lines)-1])
	}
}

func TestCLI_CancelStopsLongRunningTask(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	codexPath := writeScript(t, filepath.Dir(binaryPath), "fake-codex.sh", "#!/bin/sh\nsleep 20\necho 'should not finish'\n")
	env = append(env, "ASYNC_AGENT_CODEX_BIN="+codexPath)

	submitOutput := runCommand(t, env, binaryPath, "submit", "--store-root", storeRoot, "--provider", "codex", "--task", "analyze", "--subcommand", "exec")
	taskID := mustTaskID(t, parseJSONMap(t, submitOutput), submitOutput)

	time.Sleep(500 * time.Millisecond)
	cancelOutput := runCommand(t, env, binaryPath, "cancel", "--store-root", storeRoot, taskID)
	cancelPayload := parseJSONMap(t, cancelOutput)
	if cancelPayload["status"] != "cancelled" {
		t.Fatalf("expected cancelled status, got %v: %s", cancelPayload["status"], cancelOutput)
	}

	statusOutput := runCommand(t, env, binaryPath, "status", "--store-root", storeRoot, taskID)
	statusPayload := parseJSONMap(t, statusOutput)
	if statusPayload["status"] != "cancelled" {
		t.Fatalf("expected cancelled status after cancel, got %v: %s", statusPayload["status"], statusOutput)
	}
}

func TestMCP_InitializeAndToolsList(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)

	cmd := exec.Command(binaryPath, "mcp", "--store-root", storeRoot)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		"",
	}, "\n"))

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run mcp handshake: %v\n%s", err, output)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 MCP responses, got %d: %s", len(lines), output)
	}

	first := parseJSONMap(t, []byte(lines[0]))
	second := parseJSONMap(t, []byte(lines[1]))

	result1, _ := first["result"].(map[string]any)
	serverInfo, _ := result1["serverInfo"].(map[string]any)
	if serverInfo["name"] != "async-agent-backend" {
		t.Fatalf("unexpected server info: %s", lines[0])
	}

	result2, _ := second["result"].(map[string]any)
	tools, _ := result2["tools"].([]any)
	if len(tools) < 6 {
		t.Fatalf("expected MCP tools list, got %s", lines[1])
	}
	toolNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		entry, _ := tool.(map[string]any)
		if name, ok := entry["name"].(string); ok {
			toolNames = append(toolNames, name)
		}
	}
	for _, required := range []string{"async_agent_run_normalized", "async_agent_wait_normalized"} {
		if !containsString(toolNames, required) {
			t.Fatalf("expected MCP tool %q, got %v", required, toolNames)
		}
	}
}

func TestMCP_RunNormalizedToolReturnsSlimEnvelope(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	claudePath := writeScript(t, filepath.Dir(binaryPath), "fake-claude-mcp-normalized.sh", "#!/bin/sh\necho '{\"summary\":\"ok\"}'\n")
	env = append(env, "ASYNC_AGENT_CLAUDE_BIN="+claudePath)

	lines := runMCPSequence(t, env, binaryPath, storeRoot, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"async_agent_run_normalized","arguments":{"provider":"claude","task":"normalized","output_format":"json"}}}`,
	})
	if len(lines) < 2 {
		t.Fatalf("expected MCP responses, got %v", lines)
	}
	response := parseJSONMap(t, []byte(lines[1]))
	result, _ := response["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("unexpected content payload: %s", lines[1])
	}
	entry, _ := content[0].(map[string]any)
	text, _ := entry["text"].(string)
	payload := parseJSONMap(t, []byte(text))
	if _, ok := payload["normalized_result"]; !ok {
		t.Fatalf("expected normalized_result in MCP payload: %s", text)
	}
	if _, ok := payload["result"]; ok {
		t.Fatalf("did not expect compatibility result field in MCP normalized payload: %s", text)
	}
}

func TestCLI_ClaudeStructuredFlagsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "claude-args.txt")
	claudePath := writeScript(t, filepath.Dir(binaryPath), "fake-claude-args.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho '{\"ok\":true}'\n")
	env = append(env,
		"ASYNC_AGENT_CLAUDE_BIN="+claudePath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "claude",
		"--task", "inspect repo",
		"--output-format", "json",
		"--json-schema", "{\"type\":\"object\"}",
		"--continue",
		"--resume", "session-123",
		"--max-turns", "4",
		"--add-dir", "/tmp/project-a",
		"--add-dir", "/tmp/project-b",
		"--allowed-tool", "Read",
		"--allowed-tool", "Edit",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"--print",
		"inspect repo",
		"--output-format",
		"json",
		"--json-schema",
		"{\"type\":\"object\"}",
		"--continue",
		"--resume",
		"session-123",
		"--max-turns",
		"4",
		"--add-dir",
		"/tmp/project-a",
		"/tmp/project-b",
		"--allowedTools",
		"Read",
		"Edit",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in claude args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_CodexExecFlagsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "codex-args.txt")
	codexPath := writeScript(t, filepath.Dir(binaryPath), "fake-codex-args.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho 'codex ok'\n")
	env = append(env,
		"ASYNC_AGENT_CODEX_BIN="+codexPath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "codex",
		"--task", "review diff",
		"--subcommand", "exec",
		"--sandbox-mode", "workspace-write",
		"--approval-mode", "never",
		"--search",
		"--ephemeral",
		"--add-dir", "/tmp/extra-dir",
		"--output-last-message", "/tmp/final.txt",
		"--output-schema", "/tmp/schema.json",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"exec",
		"review diff",
		"--sandbox",
		"workspace-write",
		"--ask-for-approval",
		"never",
		"--search",
		"--ephemeral",
		"--add-dir",
		"/tmp/extra-dir",
		"--output-last-message",
		"/tmp/final.txt",
		"--output-schema",
		"/tmp/schema.json",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in codex args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_GeminiHeadlessFlagsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "gemini-args.txt")
	geminiPath := writeScript(t, filepath.Dir(binaryPath), "fake-gemini-args.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho '{\"response\":\"gemini ok\"}'\n")
	env = append(env,
		"ASYNC_AGENT_GEMINI_BIN="+geminiPath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "gemini",
		"--task", "inspect logs",
		"--output-format", "json",
		"--approval-mode", "auto_edit",
		"--sandbox",
		"--resume", "latest",
		"--include-directory", "/tmp/work-a",
		"--include-directory", "/tmp/work-b",
		"--allowed-mcp-server-name", "filesystem",
		"--allowed-mcp-server-name", "browser",
		"--extension", "foo",
		"--extension", "bar",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"--output-format",
		"json",
		"--approval-mode",
		"auto_edit",
		"--sandbox",
		"--resume",
		"latest",
		"--include-directories",
		"/tmp/work-a",
		"/tmp/work-b",
		"--allowed-mcp-server-names",
		"filesystem",
		"browser",
		"--extensions",
		"foo",
		"bar",
		"inspect logs",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in gemini args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_ClaudePermissionFlagsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "claude-permission-args.txt")
	claudePath := writeScript(t, filepath.Dir(binaryPath), "fake-claude-permission.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho '{\"ok\":true}'\n")
	env = append(env,
		"ASYNC_AGENT_CLAUDE_BIN="+claudePath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "claude",
		"--task", "permissions",
		"--tools-mode", "default",
		"--disallowed-tool", "Bash",
		"--disallowed-tool", "Write",
		"--permission-mode", "acceptEdits",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"--tools",
		"default",
		"--disallowedTools",
		"Bash",
		"Write",
		"--permission-mode",
		"acceptEdits",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in claude args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_CodexResumeFlagsAndWorkingDirAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	workingDir := filepath.Join(filepath.Dir(binaryPath), "codex-workdir")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("create codex working dir: %v", err)
	}
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "codex-resume-args.txt")
	codexPath := writeScript(t, filepath.Dir(binaryPath), "fake-codex-resume.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho 'codex resume ok'\n")
	env = append(env,
		"ASYNC_AGENT_CODEX_BIN="+codexPath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "codex",
		"--subcommand", "resume",
		"--resume", "session-99",
		"--resume-last",
		"--working-dir", workingDir,
		"--task", "continue review",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"exec",
		"resume",
		"--last",
		"-C",
		workingDir,
		"session-99",
		"continue review",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in codex resume args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_GeminiDebugFlagsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "gemini-debug-args.txt")
	geminiPath := writeScript(t, filepath.Dir(binaryPath), "fake-gemini-debug.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho '{\"response\":\"gemini debug ok\"}'\n")
	env = append(env,
		"ASYNC_AGENT_GEMINI_BIN="+geminiPath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "gemini",
		"--task", "debug run",
		"--debug",
		"--experimental-acp",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"--debug",
		"--experimental-acp",
		"--prompt",
		"debug run",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in gemini args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_ClaudeBudgetAndPermissionPromptFlagsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "claude-budget-args.txt")
	claudePath := writeScript(t, filepath.Dir(binaryPath), "fake-claude-budget.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho '{\"ok\":true}'\n")
	env = append(env,
		"ASYNC_AGENT_CLAUDE_BIN="+claudePath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "claude",
		"--task", "budgeted permissions",
		"--max-budget-usd", "1.25",
		"--permission-prompt-tool", "mcp__perm__prompt",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"--max-budget-usd",
		"1.25",
		"--permission-prompt-tool",
		"mcp__perm__prompt",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in claude args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_CodexAutomationFlagsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "codex-automation-args.txt")
	codexPath := writeScript(t, filepath.Dir(binaryPath), "fake-codex-automation.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho 'codex automation ok'\n")
	env = append(env,
		"ASYNC_AGENT_CODEX_BIN="+codexPath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "codex",
		"--task", "automation run",
		"--subcommand", "exec",
		"--full-auto",
		"--image", "/tmp/a.png",
		"--image", "/tmp/b.png",
		"--profile", "ci-profile",
		"--config-override", "model_reasoning_effort=high",
		"--config-override", "approval_policy=never",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"--full-auto",
		"--image",
		"/tmp/a.png,/tmp/b.png",
		"--profile",
		"ci-profile",
		"--config",
		"model_reasoning_effort=high",
		"approval_policy=never",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in codex args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_GeminiScreenReaderAndSessionCommandsAreMapped(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	argsOutPath := filepath.Join(filepath.Dir(binaryPath), "gemini-session-args.txt")
	geminiPath := writeScript(t, filepath.Dir(binaryPath), "fake-gemini-session.sh", "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$ARGS_OUT\"\necho '{\"response\":\"gemini sessions ok\"}'\n")
	env = append(env,
		"ASYNC_AGENT_GEMINI_BIN="+geminiPath,
		"ARGS_OUT="+argsOutPath,
	)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "gemini",
		"--screen-reader",
		"--list-sessions",
		"--delete-session", "latest",
	)

	payload := parseJSONMap(t, output)
	if payload["status"] != "succeeded" {
		t.Fatalf("expected succeeded status, got %v: %s", payload["status"], output)
	}

	argsText := readText(t, argsOutPath)
	for _, expected := range []string{
		"--screen-reader",
		"--list-sessions",
		"--delete-session",
		"latest",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected %q in gemini args, got:\n%s", expected, argsText)
		}
	}
}

func TestCLI_NormalizedResultForTextOutput(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	codexPath := writeScript(t, filepath.Dir(binaryPath), "fake-codex-text.sh", "#!/bin/sh\necho 'plain text output'\n")
	env = append(env, "ASYNC_AGENT_CODEX_BIN="+codexPath)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "codex",
		"--task", "plain text",
		"--subcommand", "exec",
	)

	payload := parseJSONMap(t, output)
	normalized := mustNestedMap(t, payload, "normalized_result")
	if normalized["output_mode"] != "text" {
		t.Fatalf("expected text output mode, got %v: %s", normalized["output_mode"], output)
	}
	if normalized["final_text"] != "plain text output" {
		t.Fatalf("unexpected normalized final text: %s", output)
	}
	if payload["result"] != "plain text output" {
		t.Fatalf("expected top-level result compatibility field, got %s", output)
	}
	normalizedPath, _ := payload["normalized_result_path"].(string)
	if normalizedPath == "" {
		t.Fatalf("expected normalized_result_path, got %s", output)
	}
	normalizedFile := parseJSONMap(t, []byte(readText(t, normalizedPath)))
	if normalizedFile["output_mode"] != "text" {
		t.Fatalf("unexpected normalized result file content: %s", readText(t, normalizedPath))
	}
}

func TestCLI_NormalizedResultForJSONOutput(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	claudePath := writeScript(t, filepath.Dir(binaryPath), "fake-claude-json.sh", "#!/bin/sh\necho '{\"summary\":\"ok\",\"count\":2}'\n")
	env = append(env, "ASYNC_AGENT_CLAUDE_BIN="+claudePath)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "claude",
		"--task", "json output",
		"--output-format", "json",
	)

	payload := parseJSONMap(t, output)
	normalized := mustNestedMap(t, payload, "normalized_result")
	if normalized["output_mode"] != "json" {
		t.Fatalf("expected json output mode, got %v: %s", normalized["output_mode"], output)
	}
	structured := mustNestedMap(t, normalized, "structured_output")
	if structured["summary"] != "ok" {
		t.Fatalf("unexpected structured output: %s", output)
	}
	if count, ok := structured["count"].(float64); !ok || count != 2 {
		t.Fatalf("unexpected structured count: %s", output)
	}
	if _, ok := payload["normalized_result_path"].(string); !ok {
		t.Fatalf("expected normalized_result_path in payload: %s", output)
	}
}

func TestCLI_NormalizedResultForStreamJSONOutput(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	geminiPath := writeScript(t, filepath.Dir(binaryPath), "fake-gemini-stream.sh", "#!/bin/sh\necho '{\"type\":\"message\",\"text\":\"hello\"}'\necho '{\"type\":\"result\",\"result\":\"done\"}'\n")
	env = append(env, "ASYNC_AGENT_GEMINI_BIN="+geminiPath)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "gemini",
		"--task", "stream output",
		"--output-format", "stream-json",
	)

	payload := parseJSONMap(t, output)
	normalized := mustNestedMap(t, payload, "normalized_result")
	if normalized["output_mode"] != "stream-json" {
		t.Fatalf("expected stream-json output mode, got %v: %s", normalized["output_mode"], output)
	}
	if normalized["final_text"] != "hello\ndone" {
		t.Fatalf("unexpected stream final text: %s", output)
	}
	if count, ok := normalized["stream_event_count"].(float64); !ok || count != 2 {
		t.Fatalf("unexpected stream event count: %s", output)
	}
	types, ok := normalized["stream_event_types"].([]any)
	if !ok || len(types) == 0 {
		t.Fatalf("expected stream event types: %s", output)
	}
	events, ok := normalized["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("expected normalized event array: %s", output)
	}
	firstEvent, ok := events[0].(map[string]any)
	if !ok || firstEvent["event_type"] != "message" || firstEvent["text"] != "hello" {
		t.Fatalf("unexpected first normalized event: %s", output)
	}
	secondEvent, ok := events[1].(map[string]any)
	if !ok || secondEvent["event_type"] != "result" || secondEvent["is_terminal"] != true {
		t.Fatalf("unexpected second normalized event: %s", output)
	}
}

func TestCLI_ResultModeNormalizedReturnsSlimEnvelope(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	claudePath := writeScript(t, filepath.Dir(binaryPath), "fake-claude-normalized-mode.sh", "#!/bin/sh\necho '{\"summary\":\"ok\"}'\n")
	env = append(env, "ASYNC_AGENT_CLAUDE_BIN="+claudePath)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "claude",
		"--task", "normalized only",
		"--output-format", "json",
		"--result-mode", "normalized",
	)

	payload := parseJSONMap(t, output)
	if _, ok := payload["normalized_result"]; !ok {
		t.Fatalf("expected normalized_result payload: %s", output)
	}
	if _, ok := payload["stdout_path"]; ok {
		t.Fatalf("did not expect stdout_path in normalized mode: %s", output)
	}
	if _, ok := payload["result"]; ok {
		t.Fatalf("did not expect compatibility result field in normalized mode: %s", output)
	}
}

func TestCLI_CodexJSONEventsNormalizeIntoStreamResult(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	codexPath := writeScript(t, filepath.Dir(binaryPath), "fake-codex-json.sh", "#!/bin/sh\necho '{\"type\":\"message\",\"text\":\"first\"}'\necho '{\"type\":\"result\",\"result\":\"second\"}'\n")
	env = append(env, "ASYNC_AGENT_CODEX_BIN="+codexPath)

	output := runCommand(t, env, binaryPath,
		"run",
		"--store-root", storeRoot,
		"--provider", "codex",
		"--task", "json events",
		"--subcommand", "exec",
		"--arg", "--json",
	)

	payload := parseJSONMap(t, output)
	normalized := mustNestedMap(t, payload, "normalized_result")
	if normalized["output_mode"] != "stream-json" {
		t.Fatalf("expected codex json to normalize as stream-json, got %v: %s", normalized["output_mode"], output)
	}
	if normalized["final_text"] != "first\nsecond" {
		t.Fatalf("unexpected codex normalized final text: %s", output)
	}
	if count, ok := normalized["stream_event_count"].(float64); !ok || count != 2 {
		t.Fatalf("unexpected codex event count: %s", output)
	}
	events, ok := normalized["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("expected codex normalized event array: %s", output)
	}
	firstEvent, ok := events[0].(map[string]any)
	if !ok || firstEvent["event_type"] != "message" || firstEvent["text"] != "first" {
		t.Fatalf("unexpected codex first event: %s", output)
	}
}

func TestMCP_ToolsListExposesExpandedRequestFields(t *testing.T) {
	t.Parallel()

	env, binaryPath, storeRoot := setupHarness(t)
	toolsPayload := runMCPToolsList(t, env, binaryPath, storeRoot)
	tool := mustToolByName(t, toolsPayload, "async_agent_run")
	inputSchema, _ := tool["inputSchema"].(map[string]any)
	properties, _ := inputSchema["properties"].(map[string]any)

	for _, field := range []string{
		"json_schema",
		"continue_session",
		"resume_session",
		"max_turns",
		"add_dirs",
		"approval_mode",
		"sandbox_mode",
		"sandbox",
		"ephemeral",
		"search",
		"output_last_message_path",
		"output_schema_path",
		"include_directories",
		"allowed_mcp_server_names",
		"extensions",
		"disallowed_tools",
		"tools_mode",
		"permission_mode",
		"resume_last",
		"resume_all",
		"debug",
		"experimental_acp",
		"max_budget_usd",
		"permission_prompt_tool",
		"full_auto",
		"images",
		"profile",
		"config_overrides",
		"screen_reader",
		"list_sessions",
		"delete_session",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("expected MCP request schema to expose %q, got %+v", field, properties)
		}
	}
}

func setupHarness(t *testing.T) ([]string, string, string) {
	t.Helper()

	moduleRoot := moduleRoot(t)
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "async-agent-backend")
	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/async-agent-backend")
	build.Dir = moduleRoot
	build.Env = os.Environ()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, output)
	}
	return os.Environ(), binaryPath, filepath.Join(tempDir, "store")
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

func writeScript(t *testing.T, dir string, name string, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

func runCommand(t *testing.T, env []string, binary string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %v: %v\n%s", args, err, output)
	}
	return output
}

func parseJSONMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode json: %v\n%s", err, raw)
	}
	return payload
}

func mustTaskID(t *testing.T, payload map[string]any, raw []byte) string {
	t.Helper()
	taskID, ok := payload["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("missing task_id in payload: %s", raw)
	}
	return taskID
}

func mustNestedMap(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("expected nested object %q in payload %+v", key, payload)
	}
	return value
}

func readText(t *testing.T, path string) string {
	t.Helper()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(bytes)
}

func runMCPToolsList(t *testing.T, env []string, binaryPath string, storeRoot string) map[string]any {
	t.Helper()

	lines := runMCPSequence(t, env, binaryPath, storeRoot, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	})
	if len(lines) >= 2 {
		return parseJSONMap(t, []byte(lines[1]))
	}
	t.Fatalf("missing tools/list response in output: %v", lines)
	return nil
}

func mustToolByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()
	result, _ := payload["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		if tool["name"] == name {
			return tool
		}
	}
	t.Fatalf("missing tool %q in payload %+v", name, payload)
	return nil
}

func runMCPSequence(t *testing.T, env []string, binaryPath string, storeRoot string, requests []string) []string {
	t.Helper()
	cmd := exec.Command(binaryPath, "mcp", "--store-root", storeRoot)
	cmd.Env = env
	payload := append(append([]string{}, requests...), "")
	cmd.Stdin = strings.NewReader(strings.Join(payload, "\n"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run mcp sequence: %v\n%s", err, output)
	}
	return nonEmptyLines(output)
}

func nonEmptyLines(raw []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	lines := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}
