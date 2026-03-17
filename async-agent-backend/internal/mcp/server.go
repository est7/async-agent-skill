package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"async-agent-backend/internal/runner"
	"async-agent-backend/internal/task"
)

type server struct {
	runner *runner.Runner
	input  io.Reader
	output io.Writer
}

// Serve runs a minimal MCP stdio server around the backend runner.
func Serve(run *runner.Runner, input io.Reader, output io.Writer) error {
	s := server{runner: run, input: input, output: output}
	return s.serve()
}

type requestEnvelope struct {
	ID     any            `json:"id,omitempty"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

func (s server) serve() error {
	scanner := bufio.NewScanner(s.input)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req requestEnvelope
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			if sendErr := s.sendError(nil, -32700, fmt.Sprintf("invalid request: %v", err)); sendErr != nil {
				return sendErr
			}
			continue
		}
		if err := s.handle(req); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s server) handle(req requestEnvelope) error {
	switch req.Method {
	case "initialize":
		return s.sendResponse(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "async-agent-backend", "version": "0.1.0"},
		})
	case "notifications/initialized":
		return nil
	case "tools/list":
		return s.sendResponse(req.ID, map[string]any{
			"tools": []map[string]any{
				tool("async_agent_submit", "Start a background task for claude, codex or gemini.", requestSchema(false)),
				tool("async_agent_run", "Run a task synchronously and return its final result.", requestSchema(false)),
				tool("async_agent_run_normalized", "Run a task synchronously and return only the normalized result envelope.", requestSchema(false)),
				tool("async_agent_status", "Return the current state for a submitted task.", map[string]any{
					"type":       "object",
					"properties": map[string]any{"task_id": map[string]any{"type": "string"}},
					"required":   []string{"task_id"},
				}),
				tool("async_agent_wait", "Wait until a task finishes and return the final result.", map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task_id":         map[string]any{"type": "string"},
						"timeout_seconds": map[string]any{"type": "integer"},
						"result_mode":     map[string]any{"type": "string", "enum": []string{"full", "normalized"}},
					},
					"required": []string{"task_id"},
				}),
				tool("async_agent_wait_normalized", "Wait until a task finishes and return only the normalized result envelope.", map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task_id":         map[string]any{"type": "string"},
						"timeout_seconds": map[string]any{"type": "integer"},
					},
					"required": []string{"task_id"},
				}),
				tool("async_agent_logs", "Read the persisted stdout/stderr logs for a task.", map[string]any{
					"type":       "object",
					"properties": map[string]any{"task_id": map[string]any{"type": "string"}},
					"required":   []string{"task_id"},
				}),
				tool("async_agent_cancel", "Cancel a running task.", map[string]any{
					"type":       "object",
					"properties": map[string]any{"task_id": map[string]any{"type": "string"}},
					"required":   []string{"task_id"},
				}),
			},
		})
	case "tools/call":
		return s.handleToolCall(req)
	default:
		return s.sendError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s server) handleToolCall(req requestEnvelope) error {
	name, _ := req.Params["name"].(string)
	arguments, _ := req.Params["arguments"].(map[string]any)
	switch name {
	case "async_agent_submit":
		request, err := decodeRequest(arguments)
		if err != nil {
			return s.sendError(req.ID, -32602, err.Error())
		}
		result, err := s.runner.Submit(request)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, payloadFromRecord(result))
	case "async_agent_run":
		request, err := decodeRequest(arguments)
		if err != nil {
			return s.sendError(req.ID, -32602, err.Error())
		}
		result, err := s.runner.Run(request)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, renderPayload(result, request.ResultMode))
	case "async_agent_run_normalized":
		request, err := decodeRequest(arguments)
		if err != nil {
			return s.sendError(req.ID, -32602, err.Error())
		}
		result, err := s.runner.Run(request)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, renderPayload(result, "normalized"))
	case "async_agent_status":
		taskID := fmt.Sprint(arguments["task_id"])
		result, err := s.runner.Status(taskID)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, renderPayload(result, fmt.Sprint(arguments["result_mode"])))
	case "async_agent_wait":
		taskID := fmt.Sprint(arguments["task_id"])
		timeoutSeconds, _ := intFromAny(arguments["timeout_seconds"])
		result, err := s.runner.Wait(taskID, timeoutSeconds)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, renderPayload(result, fmt.Sprint(arguments["result_mode"])))
	case "async_agent_wait_normalized":
		taskID := fmt.Sprint(arguments["task_id"])
		timeoutSeconds, _ := intFromAny(arguments["timeout_seconds"])
		result, err := s.runner.Wait(taskID, timeoutSeconds)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, renderPayload(result, "normalized"))
	case "async_agent_logs":
		taskID := fmt.Sprint(arguments["task_id"])
		result, err := s.runner.Logs(taskID)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, result)
	case "async_agent_cancel":
		taskID := fmt.Sprint(arguments["task_id"])
		result, err := s.runner.Cancel(taskID)
		if err != nil {
			return s.sendError(req.ID, -32603, err.Error())
		}
		return s.sendText(req.ID, result)
	default:
		return s.sendError(req.ID, -32601, fmt.Sprintf("unknown tool: %s", name))
	}
}

func (s server) sendResponse(id any, result any) error {
	return s.writeJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s server) sendError(id any, code int, message string) error {
	return s.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}

func (s server) sendText(id any, payload any) error {
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return s.sendResponse(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(bytes)}},
	})
}

func (s server) writeJSON(payload any) error {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(s.output, string(bytes)); err != nil {
		return err
	}
	return nil
}

func decodeRequest(arguments map[string]any) (task.Request, error) {
	bytes, err := json.Marshal(arguments)
	if err != nil {
		return task.Request{}, err
	}
	var req task.Request
	if err := json.Unmarshal(bytes, &req); err != nil {
		return task.Request{}, err
	}
	if strings.TrimSpace(req.Provider) == "" {
		return task.Request{}, fmt.Errorf("provider is required")
	}
	return req, nil
}

func payloadFromRecord(rec task.Record) task.ResultPayload {
	return task.ResultPayload{
		TaskID:     rec.TaskID,
		Provider:   rec.Request.Provider,
		Status:     rec.Status,
		WorkerPID:  rec.WorkerPID,
		ChildPID:   rec.ChildPID,
		Command:    rec.CommandString,
		StartedAt:  rec.StartedAt,
		StdoutPath: rec.StdoutPath,
		StderrPath: rec.StderrPath,
		ResultPath: rec.ResultPath,
	}
}

func requestSchema(requireTaskID bool) map[string]any {
	properties := map[string]any{
		"provider":                 map[string]any{"type": "string", "enum": []string{"claude", "codex", "gemini"}},
		"result_mode":              map[string]any{"type": "string", "enum": []string{"full", "normalized"}},
		"task":                     map[string]any{"type": "string"},
		"working_dir":              map[string]any{"type": "string"},
		"model":                    map[string]any{"type": "string"},
		"output_format":            map[string]any{"type": "string", "enum": []string{"text", "json", "stream-json"}},
		"subcommand":               map[string]any{"type": "string"},
		"args":                     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"timeout_seconds":          map[string]any{"type": "integer"},
		"skip_permissions":         map[string]any{"type": "boolean"},
		"json_schema":              map[string]any{"type": "string"},
		"continue_session":         map[string]any{"type": "boolean"},
		"resume_session":           map[string]any{"type": "string"},
		"max_turns":                map[string]any{"type": "integer"},
		"add_dirs":                 map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"allowed_tools":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"disallowed_tools":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"tools_mode":               map[string]any{"type": "string"},
		"permission_mode":          map[string]any{"type": "string"},
		"max_budget_usd":           map[string]any{"type": "number"},
		"permission_prompt_tool":   map[string]any{"type": "string"},
		"approval_mode":            map[string]any{"type": "string"},
		"sandbox_mode":             map[string]any{"type": "string"},
		"sandbox":                  map[string]any{"type": "boolean"},
		"ephemeral":                map[string]any{"type": "boolean"},
		"search":                   map[string]any{"type": "boolean"},
		"resume_last":              map[string]any{"type": "boolean"},
		"resume_all":               map[string]any{"type": "boolean"},
		"full_auto":                map[string]any{"type": "boolean"},
		"images":                   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"profile":                  map[string]any{"type": "string"},
		"config_overrides":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"output_last_message_path": map[string]any{"type": "string"},
		"output_schema_path":       map[string]any{"type": "string"},
		"include_directories":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"allowed_mcp_server_names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"extensions":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"debug":                    map[string]any{"type": "boolean"},
		"experimental_acp":         map[string]any{"type": "boolean"},
		"screen_reader":            map[string]any{"type": "boolean"},
		"list_sessions":            map[string]any{"type": "boolean"},
		"delete_session":           map[string]any{"type": "string"},
	}
	required := []string{"provider", "task"}
	if requireTaskID {
		properties["task_id"] = map[string]any{"type": "string"}
		required = append(required, "task_id")
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func tool(name string, description string, schema map[string]any) map[string]any {
	return map[string]any{"name": name, "description": description, "inputSchema": schema}
}

func intFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func renderPayload(payload task.ResultPayload, mode string) any {
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
