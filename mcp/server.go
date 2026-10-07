// Package mcp implements the Model Context Protocol (MCP) server for
// micromax/sandbox, enabling AI agents and assistants (Claude Desktop, Cursor,
// OpenAI assistants, etc.) to securely execute untrusted code.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/micromax/sandbox"
)

// ProtocolVersion is the pinned MCP protocol specification version.
const ProtocolVersion = "2024-11-05"

// JSONRPCMessage is a standard JSON-RPC 2.0 envelope.
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Server provides the MCP interface for a [sandbox.Sandbox].
type Server struct {
	sb       *sandbox.Sandbox
	version  string
	mu       sync.Mutex
	sessions map[string]sandbox.Session
}

// Option configures the MCP server.
type Option func(*Server)

// WithVersion sets the reported server version.
func WithVersion(v string) Option {
	return func(s *Server) {
		s.version = v
	}
}

// NewServer creates a new MCP server backed by sb.
func NewServer(sb *sandbox.Sandbox, opts ...Option) *Server {
	s := &Server{
		sb:       sb,
		version:  "0.1.0",
		sessions: make(map[string]sandbox.Session),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Serve reads JSON-RPC messages from in and writes responses to out.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(stripBOM(in))
	encoder := json.NewEncoder(out)
	var writeMu sync.Mutex

	send := func(msg JSONRPCMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(msg)
	}

	for {
		var req JSONRPCMessage
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			_ = send(JSONRPCMessage{
				JSONRPC: "2.0",
				Error: &JSONRPCError{
					Code:    -32700,
					Message: fmt.Sprintf("parse error: %v", err),
				},
			})
			return err
		}

		// Handle notifications (no ID)
		if req.ID == nil {
			s.handleNotification(ctx, req)
			continue
		}

		// Handle requests (expects response with matching ID)
		res := s.handleRequest(ctx, req)
		if err := send(res); err != nil {
			return fmt.Errorf("sending response: %w", err)
		}
	}
}

// ServeStdio is a convenience function that runs the MCP server on os.Stdin / os.Stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.Serve(ctx, os.Stdin, os.Stdout)
}

func (s *Server) handleNotification(ctx context.Context, req JSONRPCMessage) {
	// MCP notifications like "notifications/initialized" require no response
	switch req.Method {
	case "notifications/initialized":
		// Client confirmed initialization
	}
}

func (s *Server) handleRequest(ctx context.Context, req JSONRPCMessage) JSONRPCMessage {
	resp := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "micromax-sandbox",
				"version": s.version,
			},
		}

	case "ping":
		resp.Result = map[string]any{}

	case "tools/list":
		resp.Result = map[string]any{
			"tools": s.toolDefinitions(),
		}

	case "tools/call":
		var callParams struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			resp.Error = &JSONRPCError{Code: -32602, Message: "invalid call params: " + err.Error()}
			return resp
		}

		content, isError := s.callTool(ctx, callParams.Name, callParams.Arguments)
		resp.Result = map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": content,
				},
			},
			"isError": isError,
		}

	default:
		resp.Error = &JSONRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("method not found: %s", req.Method),
		}
	}

	return resp
}

func (s *Server) toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "execute_code",
			"description": "Execute code in a strict, isolated sandbox. Languages supported: TypeScript (ts), JavaScript (js), Python (python), Lua (lua), Raw WebAssembly (wasm) in pure WebAssembly (zero Docker/CGO); and Java, Rust, Go, Bash in hardened Docker containers.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"lang": map[string]any{
						"type":        "string",
						"description": "Programming language (e.g. ts, js, python, lua, wasm, go, rust, java, bash)",
						"enum":        []string{"ts", "js", "python", "lua", "wasm", "go", "rust", "java", "bash"},
					},
					"code": map[string]any{
						"type":        "string",
						"description": "Source code to execute inside the sandbox",
					},
					"stdin": map[string]any{
						"type":        "string",
						"description": "Optional standard input string",
					},
					"timeout_seconds": map[string]any{
						"type":        "number",
						"description": "Wall-clock execution timeout in seconds (default 10s, max 60s)",
					},
					"memory_limit_mb": map[string]any{
						"type":        "number",
						"description": "Optional memory ceiling in megabytes",
					},
				},
				"required": []string{"lang", "code"},
			},
		},
		{
			"name":        "eval_session",
			"description": "Evaluate code in a stateful multi-turn interactive REPL session. Variables, imports, and state are preserved across calls within the same session_id.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{
						"type":        "string",
						"description": "Session identifier string. Use the same ID to continue an existing session.",
					},
					"lang": map[string]any{
						"type":        "string",
						"description": "Session language (ts, js, python, lua)",
						"enum":        []string{"ts", "js", "python", "lua"},
					},
					"code": map[string]any{
						"type":        "string",
						"description": "Code snippet or expression to evaluate in the persistent session",
					},
				},
				"required": []string{"session_id", "lang", "code"},
			},
		},
		{
			"name":        "list_languages",
			"description": "List all registered language packs, execution isolation tiers (Pure WebAssembly vs Hardened Docker), and capabilities.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, rawArgs json.RawMessage) (string, bool) {
	switch name {
	case "execute_code":
		var args struct {
			Lang           string  `json:"lang"`
			Code           string  `json:"code"`
			Stdin          string  `json:"stdin"`
			TimeoutSeconds float64 `json:"timeout_seconds"`
			MemoryLimitMB  float64 `json:"memory_limit_mb"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return fmt.Sprintf("Error: invalid arguments: %v", err), true
		}

		timeout := 10 * time.Second
		if args.TimeoutSeconds > 0 {
			if args.TimeoutSeconds > 60 {
				args.TimeoutSeconds = 60
			}
			timeout = time.Duration(args.TimeoutSeconds * float64(time.Second))
		}

		spec := sandbox.Spec{
			Lang: args.Lang,
			Code: args.Code,
		}
		if args.Stdin != "" {
			spec.Stdin = strings.NewReader(args.Stdin)
		}
		if args.MemoryLimitMB > 0 {
			spec.Limits = &sandbox.Limits{
				Memory: uint64(args.MemoryLimitMB * 1024 * 1024),
			}
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		res, err := s.sb.Run(execCtx, spec)
		if err != nil {
			return fmt.Sprintf("Execution Failed: %v\nExitCode: %d\nStderr:\n%s", err, res.ExitCode, string(res.Stderr)), true
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Status: Exit %d | Backend: %s | Duration: %v\n", res.ExitCode, res.Backend, res.Usage.Wall))
		if len(res.Stdout) > 0 {
			sb.WriteString("\n=== STDOUT ===\n")
			sb.WriteString(string(res.Stdout))
		}
		if len(res.Stderr) > 0 {
			sb.WriteString("\n=== STDERR ===\n")
			sb.WriteString(string(res.Stderr))
		}
		if len(res.Files) > 0 {
			sb.WriteString("\n=== GENERATED FILES ===\n")
			for k, v := range res.Files {
				sb.WriteString(fmt.Sprintf("• %s (%d bytes)\n", k, len(v)))
			}
		}
		return sb.String(), res.ExitCode != 0

	case "eval_session":
		var args struct {
			SessionID string `json:"session_id"`
			Lang      string `json:"lang"`
			Code      string `json:"code"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return fmt.Sprintf("Error: invalid arguments: %v", err), true
		}

		s.mu.Lock()
		sess, ok := s.sessions[args.SessionID]
		if !ok {
			var err error
			sess, err = s.sb.NewSession(ctx, args.Lang, sandbox.WithSessionID(args.SessionID))
			if err != nil {
				s.mu.Unlock()
				return fmt.Sprintf("Error creating session %q: %v", args.SessionID, err), true
			}
			s.sessions[args.SessionID] = sess
		}
		s.mu.Unlock()

		res, err := sess.Eval(ctx, args.Code)
		if err != nil {
			return fmt.Sprintf("Eval Error: %v\nStderr:\n%s", err, string(res.Stderr)), true
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Session: %s (%s)\n", args.SessionID, args.Lang))
		if len(res.Stdout) > 0 {
			sb.WriteString(string(res.Stdout))
		}
		if len(res.Stderr) > 0 {
			sb.WriteString("\n[stderr] " + string(res.Stderr))
		}
		return sb.String(), res.ExitCode != 0

	case "list_languages":
		var sb strings.Builder
		sb.WriteString("=== micromax/sandbox Supported Languages ===\n\n")
		sb.WriteString("Tier 1: Pure WebAssembly (Zero Docker, Zero CGO, Embedded, <50ms startup)\n")
		sb.WriteString("  • TypeScript (ts, typescript) : AST type stripping on QuickJS Wasm\n")
		sb.WriteString("  • JavaScript (js, javascript) : QuickJS-NG Wasm embedded (~1.5 MB)\n")
		sb.WriteString("  • Python (python, py)         : Official CPython 3.13 WASI\n")
		sb.WriteString("  • Lua (lua, lua54)            : Lua 5.4.6 WASI embedded (~320 KB)\n")
		sb.WriteString("  • Raw WASI (wasm, wasi)       : Precompiled Go (wasip1), Rust, C, Zig\n\n")
		sb.WriteString("Tier 2: Hardened Docker (Read-only rootfs, dropped ALL caps, universal)\n")
		sb.WriteString("  • Go (go, golang)             : Full compiler toolchain (golang:alpine)\n")
		sb.WriteString("  • Rust (rust, rs)             : rustc compilation (rust:alpine)\n")
		sb.WriteString("  • Java (java)                 : OpenJDK 21 (javac / java)\n")
		sb.WriteString("  • Bash (bash, sh)             : POSIX shell environment\n")
		sb.WriteString("  • Node.js (node, nodejs)      : Node.js runtime\n")
		return sb.String(), false

	default:
		return fmt.Sprintf("Error: unknown tool %q", name), true
	}
}

// Close closes all active interactive sessions.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		_ = sess.Close()
		delete(s.sessions, id)
	}
	return nil
}

// OpenAITools returns the JSON tool definition array for OpenAI function calling.
func OpenAITools() []map[string]any {
	defs := (&Server{}).toolDefinitions()
	tools := make([]map[string]any, len(defs))
	for i, d := range defs {
		tools[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        d["name"],
				"description": d["description"],
				"parameters":  d["inputSchema"],
			},
		}
	}
	return tools
}

// AnthropicTools returns the tool definition array formatted for Anthropic Claude.
func AnthropicTools() []map[string]any {
	defs := (&Server{}).toolDefinitions()
	tools := make([]map[string]any, len(defs))
	for i, d := range defs {
		tools[i] = map[string]any{
			"name":         d["name"],
			"description":  d["description"],
			"input_schema": d["inputSchema"],
		}
	}
	return tools
}

func stripBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	b, err := br.Peek(3)
	if err == nil && len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		_, _ = br.Discard(3)
	}
	return br
}
