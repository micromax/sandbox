package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	backendwasm "github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/mcp"
	packjs "github.com/micromax/sandbox/packs/js"
	packts "github.com/micromax/sandbox/packs/ts"
)

func setupTestServer(t *testing.T) *mcp.Server {
	t.Helper()
	wBackend, err := backendwasm.New()
	if err != nil {
		t.Fatalf("backendwasm.New: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(packjs.Pack(), packts.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	return mcp.NewServer(sb, mcp.WithVersion("0.1.0-test"))
}

func TestMCPInitializeAndListTools(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate client sending initialize request
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	if err := srv.Serve(ctx, in, &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d:\n%s", len(lines), out.String())
	}

	// 1. Check initialize response
	var initResp mcp.JSONRPCMessage
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("unmarshaling init response: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("init returned error: %+v", initResp.Error)
	}
	resMap := initResp.Result.(map[string]any)
	if resMap["protocolVersion"] != mcp.ProtocolVersion {
		t.Errorf("unexpected protocolVersion: %v", resMap["protocolVersion"])
	}

	// 2. Check tools/list response
	var listResp mcp.JSONRPCMessage
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatalf("unmarshaling list response: %v", err)
	}
	listMap := listResp.Result.(map[string]any)
	tools := listMap["tools"].([]any)
	if len(tools) < 3 {
		t.Fatalf("expected at least 3 tools, got %d", len(tools))
	}
}

func TestMCPExecuteCode(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reqJSON := `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"execute_code","arguments":{"lang":"ts","code":"interface Cat { name: string }; const c: Cat = { name: 'Luna' }; console.log('Hello ' + c.name);" }}}` + "\n"
	in := bytes.NewBufferString(reqJSON)
	var out bytes.Buffer

	if err := srv.Serve(ctx, in, &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	var callResp mcp.JSONRPCMessage
	if err := json.Unmarshal(out.Bytes(), &callResp); err != nil {
		t.Fatalf("unmarshaling call response: %v", err)
	}
	if callResp.Error != nil {
		t.Fatalf("call returned error: %+v", callResp.Error)
	}

	resMap := callResp.Result.(map[string]any)
	if resMap["isError"].(bool) {
		t.Fatalf("tool call returned isError=true")
	}

	contents := resMap["content"].([]any)
	firstContent := contents[0].(map[string]any)
	text := firstContent["text"].(string)

	if !strings.Contains(text, "Hello Luna") {
		t.Fatalf("expected 'Hello Luna' in output, got: %s", text)
	}
}

func TestMCPEvalSession(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Set variable in session
	req1 := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"eval_session","arguments":{"session_id":"test-sess-1","lang":"js","code":"var counter = 40;" }}}` + "\n"
	// 2. Read and modify variable in same session
	req2 := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"eval_session","arguments":{"session_id":"test-sess-1","lang":"js","code":"counter += 2; console.log('COUNTER=' + counter);" }}}` + "\n"

	in := bytes.NewBufferString(req1 + req2)
	var out bytes.Buffer

	if err := srv.Serve(ctx, in, &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d:\n%s", len(lines), out.String())
	}

	var resp2 mcp.JSONRPCMessage
	if err := json.Unmarshal([]byte(lines[1]), &resp2); err != nil {
		t.Fatalf("unmarshaling response 2: %v", err)
	}

	resMap := resp2.Result.(map[string]any)
	contents := resMap["content"].([]any)
	text := contents[0].(map[string]any)["text"].(string)

	if !strings.Contains(text, "COUNTER=42") {
		t.Fatalf("expected 'COUNTER=42' in session response, got: %s", text)
	}
}

func TestToolDefinitionsExport(t *testing.T) {
	oaTools := mcp.OpenAITools()
	if len(oaTools) != 3 {
		t.Errorf("expected 3 OpenAI tools, got %d", len(oaTools))
	}

	anthTools := mcp.AnthropicTools()
	if len(anthTools) != 3 {
		t.Errorf("expected 3 Anthropic tools, got %d", len(anthTools))
	}
}
