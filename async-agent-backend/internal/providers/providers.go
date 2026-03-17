package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"async-agent-backend/internal/task"
)

const (
	envClaudeBin = "ASYNC_AGENT_CLAUDE_BIN"
	envCodexBin  = "ASYNC_AGENT_CODEX_BIN"
	envGeminiBin = "ASYNC_AGENT_GEMINI_BIN"
)

// Provider builds provider-specific commands and extracts their final result.
type Provider interface {
	Name() string
	Validate(task.Request) error
	BuildCommand(task.Request) ([]string, error)
	ExtractResult(stdout string, stderr string, req task.Request) task.NormalizedResult
}

// Registry returns the requested provider implementation.
func Registry(name string) (Provider, error) {
	switch strings.ToLower(name) {
	case "claude":
		return claudeProvider{}, nil
	case "codex":
		return codexProvider{}, nil
	case "gemini":
		return geminiProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}

type claudeProvider struct{}
type codexProvider struct{}
type geminiProvider struct{}

func (claudeProvider) Name() string { return "claude" }
func (codexProvider) Name() string  { return "codex" }
func (geminiProvider) Name() string { return "gemini" }

func (claudeProvider) Validate(req task.Request) error {
	if strings.TrimSpace(req.Task) == "" {
		return errors.New("claude task is required")
	}
	return nil
}

func (claudeProvider) BuildCommand(req task.Request) ([]string, error) {
	cmd := []string{binaryOrDefault(envClaudeBin, "claude"), "--print", req.Task}
	outputFormat := req.OutputFormat
	if outputFormat == "" {
		outputFormat = "stream-json"
	}
	if outputFormat != "text" {
		cmd = append(cmd, "--output-format", outputFormat)
		if outputFormat == "stream-json" {
			cmd = append(cmd, "--verbose")
		}
	}
	if req.Model != "" {
		cmd = append(cmd, "--model", req.Model)
	}
	if req.JSONSchema != "" {
		cmd = append(cmd, "--json-schema", req.JSONSchema)
	}
	if req.ContinueSession {
		cmd = append(cmd, "--continue")
	}
	if req.ResumeSession != "" {
		cmd = append(cmd, "--resume", req.ResumeSession)
	}
	if req.MaxTurns > 0 {
		cmd = append(cmd, "--max-turns", fmt.Sprintf("%d", req.MaxTurns))
	}
	for _, dir := range req.AddDirs {
		if strings.TrimSpace(dir) != "" {
			cmd = append(cmd, "--add-dir", dir)
		}
	}
	if len(req.AllowedTools) > 0 {
		cmd = append(cmd, "--allowedTools", strings.Join(req.AllowedTools, ","))
	}
	if len(req.DisallowedTools) > 0 {
		cmd = append(cmd, "--disallowedTools", strings.Join(req.DisallowedTools, ","))
	}
	if req.ToolsMode != "" {
		cmd = append(cmd, "--tools", req.ToolsMode)
	}
	if req.PermissionMode != "" {
		cmd = append(cmd, "--permission-mode", req.PermissionMode)
	}
	if req.MaxBudgetUSD > 0 {
		cmd = append(cmd, "--max-budget-usd", fmt.Sprintf("%.2f", req.MaxBudgetUSD))
	}
	if req.PermissionPromptTool != "" {
		cmd = append(cmd, "--permission-prompt-tool", req.PermissionPromptTool)
	}
	skipPermissions := true
	if req.SkipPermissions != nil {
		skipPermissions = *req.SkipPermissions
	}
	if skipPermissions {
		cmd = append(cmd, "--dangerously-skip-permissions")
	}
	cmd = append(cmd, req.Args...)
	return cmd, nil
}

func (claudeProvider) ExtractResult(stdout string, stderr string, req task.Request) task.NormalizedResult {
	outputFormat := req.OutputFormat
	if outputFormat == "" {
		outputFormat = "stream-json"
	}
	result := task.NormalizedResult{OutputMode: outputFormat}
	if outputFormat == "stream-json" {
		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		types := make([]string, 0, len(lines))
		events := make([]task.NormalizedEvent, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				continue
			}
			result.StreamEventCount++
			if eventType, ok := payload["type"].(string); ok && eventType != "" && !contains(types, eventType) {
				types = append(types, eventType)
			}
			events = append(events, normalizeEvent(payload))
			if payload["type"] == "result" {
				if finalText, ok := payload["result"].(string); ok && strings.TrimSpace(finalText) != "" {
					return task.NormalizedResult{
						OutputMode:       outputFormat,
						FinalText:        finalText,
						StreamEventCount: result.StreamEventCount,
						StreamEventTypes: types,
						Events:           events,
					}
				}
			}
		}
		if trimmed := strings.TrimSpace(stdout); trimmed != "" {
			result.FinalText = trimmed
			result.StreamEventTypes = types
			result.Events = events
			return result
		}
	}
	if outputFormat == "json" {
		var payload any
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err == nil {
			bytes, _ := json.MarshalIndent(payload, "", "  ")
			result.StructuredOutput = payload
			result.FinalText = string(bytes)
			return result
		}
	}
	if trimmed := strings.TrimSpace(stdout); trimmed != "" {
		result.OutputMode = "text"
		result.FinalText = trimmed
		return result
	}
	if trimmed := strings.TrimSpace(stderr); trimmed != "" {
		result.OutputMode = "text"
		result.FinalText = trimmed
		return result
	}
	return task.NormalizedResult{OutputMode: "text", FinalText: "No output from Claude Code"}
}

func (codexProvider) Validate(req task.Request) error {
	if req.Subcommand == "" {
		req.Subcommand = "exec"
	}
	valid := map[string]bool{"exec": true, "apply": true, "resume": true, "sandbox": true}
	if !valid[req.Subcommand] {
		return fmt.Errorf("unsupported codex subcommand %q", req.Subcommand)
	}
	return nil
}

func (codexProvider) BuildCommand(req task.Request) ([]string, error) {
	subcommand := req.Subcommand
	if subcommand == "" {
		subcommand = "exec"
	}
	cmd := []string{binaryOrDefault(envCodexBin, "codex")}
	if req.WorkingDir != "" {
		cmd = append(cmd, "-C", req.WorkingDir)
	}
	if subcommand == "resume" {
		cmd = append(cmd, "exec", "resume")
	} else {
		cmd = append(cmd, subcommand)
	}
	if req.SandboxMode != "" {
		cmd = append(cmd, "--sandbox", req.SandboxMode)
	}
	if req.ApprovalMode != "" {
		cmd = append(cmd, "--ask-for-approval", req.ApprovalMode)
	}
	if req.Search {
		cmd = append(cmd, "--search")
	}
	if req.FullAuto {
		cmd = append(cmd, "--full-auto")
	}
	if req.Ephemeral {
		cmd = append(cmd, "--ephemeral")
	}
	if req.ResumeLast {
		cmd = append(cmd, "--last")
	}
	if req.ResumeAll {
		cmd = append(cmd, "--all")
	}
	for _, dir := range req.AddDirs {
		if strings.TrimSpace(dir) != "" {
			cmd = append(cmd, "--add-dir", dir)
		}
	}
	if len(req.Images) > 0 {
		cmd = append(cmd, "--image", strings.Join(req.Images, ","))
	}
	if req.Profile != "" {
		cmd = append(cmd, "--profile", req.Profile)
	}
	for _, override := range req.ConfigOverrides {
		if strings.TrimSpace(override) != "" {
			cmd = append(cmd, "--config", override)
		}
	}
	if req.OutputLastMessagePath != "" {
		cmd = append(cmd, "--output-last-message", req.OutputLastMessagePath)
	}
	if req.OutputSchemaPath != "" {
		cmd = append(cmd, "--output-schema", req.OutputSchemaPath)
	}
	cmd = append(cmd, req.Args...)
	if subcommand == "resume" && req.ResumeSession != "" {
		cmd = append(cmd, req.ResumeSession)
	}
	if req.Task != "" {
		cmd = append(cmd, req.Task)
	}
	if !contains(cmd, "--skip-git-repo-check") {
		cmd = append(cmd, "--skip-git-repo-check")
	}
	return cmd, nil
}

func (codexProvider) ExtractResult(stdout string, stderr string, req task.Request) task.NormalizedResult {
	if contains(req.Args, "--json") || contains(req.Args, "--experimental-json") {
		return normalizeStreamJSON(stdout, stderr, "No output from Codex")
	}
	if trimmed := strings.TrimSpace(stdout); trimmed != "" {
		return task.NormalizedResult{OutputMode: "text", FinalText: trimmed}
	}
	matcher := regexp.MustCompile(`codex\n(.+?)(?:tokens used|\z)`)
	if matches := matcher.FindStringSubmatch(stderr); len(matches) > 1 {
		return task.NormalizedResult{OutputMode: "text", FinalText: strings.TrimSpace(matches[1])}
	}
	return task.NormalizedResult{OutputMode: "text", FinalText: "No output from Codex"}
}

func (geminiProvider) Validate(req task.Request) error {
	if strings.TrimSpace(req.Task) == "" && !req.ListSessions && strings.TrimSpace(req.DeleteSession) == "" {
		return errors.New("gemini task is required")
	}
	return nil
}

func (geminiProvider) BuildCommand(req task.Request) ([]string, error) {
	cmd := []string{binaryOrDefault(envGeminiBin, "gemini")}
	if req.OutputFormat != "" {
		cmd = append(cmd, "--output-format", req.OutputFormat)
	}
	if req.Model != "" {
		cmd = append(cmd, "--model", req.Model)
	}
	if req.Debug {
		cmd = append(cmd, "--debug")
	}
	if req.ExperimentalACP {
		cmd = append(cmd, "--experimental-acp")
	}
	if req.ScreenReader {
		cmd = append(cmd, "--screen-reader")
	}
	if req.ListSessions {
		cmd = append(cmd, "--list-sessions")
	}
	if req.DeleteSession != "" {
		cmd = append(cmd, "--delete-session", req.DeleteSession)
	}
	if req.ApprovalMode != "" {
		cmd = append(cmd, "--approval-mode", req.ApprovalMode)
	}
	if req.Sandbox {
		cmd = append(cmd, "--sandbox")
	}
	if req.ResumeSession != "" {
		cmd = append(cmd, "--resume", req.ResumeSession)
	}
	for _, dir := range req.IncludeDirectories {
		if strings.TrimSpace(dir) != "" {
			cmd = append(cmd, "--include-directories", dir)
		}
	}
	for _, serverName := range req.AllowedMCPServerNames {
		if strings.TrimSpace(serverName) != "" {
			cmd = append(cmd, "--allowed-mcp-server-names", serverName)
		}
	}
	for _, extension := range req.Extensions {
		if strings.TrimSpace(extension) != "" {
			cmd = append(cmd, "--extensions", extension)
		}
	}
	cmd = append(cmd, req.Args...)
	if req.Task != "" {
		cmd = append(cmd, "--prompt", req.Task)
	}
	return cmd, nil
}

func (geminiProvider) ExtractResult(stdout string, stderr string, req task.Request) task.NormalizedResult {
	if req.OutputFormat == "json" {
		var payload any
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err == nil {
			bytes, _ := json.MarshalIndent(payload, "", "  ")
			return task.NormalizedResult{
				OutputMode:       "json",
				FinalText:        string(bytes),
				StructuredOutput: payload,
			}
		}
	}
	if req.OutputFormat == "stream-json" {
		lines := strings.Split(stdout, "\n")
		collected := make([]string, 0, len(lines))
		eventTypes := make([]string, 0, len(lines))
		events := make([]task.NormalizedEvent, 0, len(lines))
		eventCount := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(line), &payload); err == nil {
				eventCount++
				if eventType, ok := payload["type"].(string); ok && eventType != "" && !contains(eventTypes, eventType) {
					eventTypes = append(eventTypes, eventType)
				}
				events = append(events, normalizeEvent(payload))
				for _, key := range []string{"text", "content", "result"} {
					if text, ok := payload[key].(string); ok && text != "" {
						collected = append(collected, text)
						goto nextLine
					}
				}
				goto nextLine
			}
			collected = append(collected, line)
		nextLine:
		}
		if len(collected) > 0 {
			return task.NormalizedResult{
				OutputMode:       "stream-json",
				FinalText:        strings.Join(collected, "\n"),
				StreamEventCount: eventCount,
				StreamEventTypes: eventTypes,
				Events:           events,
			}
		}
	}
	if trimmed := strings.TrimSpace(stdout); trimmed != "" {
		return task.NormalizedResult{OutputMode: "text", FinalText: trimmed}
	}
	if trimmed := strings.TrimSpace(stderr); trimmed != "" {
		return task.NormalizedResult{OutputMode: "text", FinalText: trimmed}
	}
	return task.NormalizedResult{OutputMode: "text", FinalText: "No output from Gemini CLI"}
}

func normalizeStreamJSON(stdout string, stderr string, fallback string) task.NormalizedResult {
	lines := strings.Split(stdout, "\n")
	collected := make([]string, 0, len(lines))
	eventTypes := make([]string, 0, len(lines))
	events := make([]task.NormalizedEvent, 0, len(lines))
	eventCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err == nil {
			eventCount++
			if eventType, ok := payload["type"].(string); ok && eventType != "" && !contains(eventTypes, eventType) {
				eventTypes = append(eventTypes, eventType)
			}
			events = append(events, normalizeEvent(payload))
			for _, key := range []string{"text", "content", "result", "message"} {
				if text, ok := payload[key].(string); ok && text != "" {
					collected = append(collected, text)
					goto nextLine
				}
			}
			goto nextLine
		}
		collected = append(collected, line)
	nextLine:
	}
	if len(collected) > 0 {
		return task.NormalizedResult{
			OutputMode:       "stream-json",
			FinalText:        strings.Join(collected, "\n"),
			StreamEventCount: eventCount,
			StreamEventTypes: eventTypes,
			Events:           events,
		}
	}
	if trimmed := strings.TrimSpace(stderr); trimmed != "" {
		return task.NormalizedResult{OutputMode: "text", FinalText: trimmed}
	}
	return task.NormalizedResult{OutputMode: "text", FinalText: fallback}
}

func binaryOrDefault(envVar string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
		return value
	}
	return fallback
}

func contains(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func normalizeEvent(payload map[string]any) task.NormalizedEvent {
	rawType, _ := payload["type"].(string)
	eventType := rawType
	if eventType == "" {
		switch {
		case hasStringKey(payload, "result"):
			eventType = "result"
		case hasStringKey(payload, "text"), hasStringKey(payload, "content"), hasStringKey(payload, "message"):
			eventType = "message"
		default:
			eventType = "other"
		}
	}

	text := firstString(payload, "text", "content", "result", "message", "error")
	toolName := firstString(payload, "name", "tool_name", "tool")
	isError := eventType == "error"
	isTerminal := eventType == "result" || isError
	if eventType == "init" {
		eventType = "meta"
	}

	return task.NormalizedEvent{
		EventType:  eventType,
		RawType:    rawType,
		Text:       text,
		ToolName:   toolName,
		IsError:    isError,
		IsTerminal: isTerminal,
	}
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func hasStringKey(payload map[string]any, key string) bool {
	value, ok := payload[key].(string)
	return ok && value != ""
}
