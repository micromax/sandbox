package sandbox_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/js"
)

func TestSSEHandler(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("wasm.New failed: %v", err)
	}
	defer wBackend.Close(context.Background())

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack()),
	)
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}

	ts := httptest.NewServer(sb.SSEHandler())
	defer ts.Close()

	payload, err := json.Marshal(sandbox.Spec{
		Lang: "js",
		Code: `console.log("LINE 1"); console.log("LINE 2");`,
	})
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}

	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got: %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got: %s", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	var events []string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimPrefix(line, "event: "))
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("reading SSE stream: %v", err)
	}

	hasStart := false
	hasStdout := false
	hasDone := false

	for _, ev := range events {
		switch ev {
		case "start":
			hasStart = true
		case "stdout":
			hasStdout = true
		case "done":
			hasDone = true
		}
	}

	if !hasStart {
		t.Errorf("missing 'start' event in SSE stream: %v", events)
	}
	if !hasStdout {
		t.Errorf("missing 'stdout' event in SSE stream: %v", events)
	}
	if !hasDone {
		t.Errorf("missing 'done' event in SSE stream: %v", events)
	}
}
