package sandbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SSEHandler returns an http.Handler that executes code on the sandbox and
// streams stdout, stderr, and lifecycle events to the client in real time
// via Server-Sent Events (SSE).
func (s *Sandbox) SSEHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed (use POST)", http.StatusMethodNotAllowed)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported by response writer", http.StatusBadRequest)
			return
		}

		var spec Spec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			http.Error(w, fmt.Sprintf("invalid spec: %v", err), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		sendEvent := func(event string, data any) {
			dataBytes, _ := json.Marshal(data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(dataBytes))
			flusher.Flush()
		}

		sendEvent("start", map[string]any{
			"lang":      spec.Lang,
			"timestamp": time.Now().UTC(),
		})

		spec.OnStdout = func(chunk []byte) {
			sendEvent("stdout", map[string]any{
				"text": string(chunk),
			})
		}

		spec.OnStderr = func(chunk []byte) {
			sendEvent("stderr", map[string]any{
				"text": string(chunk),
			})
		}

		start := time.Now()
		res, err := s.Run(r.Context(), spec)
		wall := time.Since(start)

		if err != nil {
			sendEvent("error", map[string]any{
				"error":    err.Error(),
				"duration": wall.String(),
			})
			return
		}

		sendEvent("done", map[string]any{
			"exit_code": res.ExitCode,
			"duration":  res.Usage.Wall.String(),
			"backend":   res.Backend,
		})
	})
}
