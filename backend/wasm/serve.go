package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/micromax/sandbox"
)

var _ sandbox.ServeBackend = (*Backend)(nil)

const jsServeBridge = `
function __bridge_fetch(reqStr) {
    try {
        var req = JSON.parse(reqStr);
        var handler = null;
        if (typeof handleRequest === 'function') {
            handler = handleRequest;
        } else if (typeof fetch === 'function') {
            handler = fetch;
        } else if (typeof app === 'function') {
            handler = app;
        } else if (typeof defaultExport !== 'undefined' && typeof defaultExport.fetch === 'function') {
            handler = defaultExport.fetch;
        }

        if (!handler) {
            return JSON.stringify({
                status: 500,
                headers: {"Content-Type": "text/plain"},
                body: "No fetch(req) or handleRequest(req) handler found"
            });
        }

        var res = handler(req);
        var status = 200;
        var headers = {"Content-Type": "text/plain"};
        var body = "";

        if (typeof res === 'string') {
            body = res;
        } else if (res && typeof res === 'object') {
            status = res.status || 200;
            if (res.headers) {
                headers = res.headers;
            }
            body = res.body !== undefined ? String(res.body) : JSON.stringify(res);
        } else if (res !== undefined) {
            body = String(res);
        }

        return JSON.stringify({ status: status, headers: headers, body: body });
    } catch (e) {
        return JSON.stringify({
            status: 500,
            headers: {"Content-Type": "text/plain"},
            body: String(e && e.stack ? e.stack : e)
        });
    }
}
`

const pyServeBridge = `
import json, sys

def __bridge_http(req_str):
    try:
        req = json.loads(req_str)
        # 1. WSGI callable app(environ, start_response)
        if 'app' in globals() and callable(globals()['app']):
            status_holder = {'status': 200, 'headers': {}}
            def start_response(status, headers):
                status_holder['status'] = int(status.split()[0])
                for k, v in headers:
                    status_holder['headers'][k] = v
                return None

            import io
            body_bytes = req.get('body', '').encode('utf-8')
            environ = {
                'REQUEST_METHOD': req.get('method', 'GET'),
                'PATH_INFO': req.get('path', '/'),
                'QUERY_STRING': req.get('query', ''),
                'SERVER_PROTOCOL': 'HTTP/1.1',
                'wsgi.input': io.BytesIO(body_bytes),
                'wsgi.errors': io.StringIO(),
                'wsgi.version': (1, 0),
                'wsgi.multithread': False,
                'wsgi.multiprocess': False,
                'wsgi.run_once': False,
            }
            for k, v in req.get('headers', {}).items():
                environ['HTTP_' + k.upper().replace('-', '_')] = v

            chunks = globals()['app'](environ, start_response)
            body = b"".join([c.encode('utf-8') if isinstance(c, str) else c for c in chunks])
            sys.stdout.write(json.dumps({
                'status': status_holder['status'],
                'headers': status_holder['headers'],
                'body': body.decode('utf-8', errors='replace')
            }) + "\n")
            return None

        # 2. fetch(req) function
        elif 'fetch' in globals() and callable(globals()['fetch']):
            res = globals()['fetch'](req)
            if isinstance(res, dict):
                sys.stdout.write(json.dumps({
                    'status': res.get('status', 200),
                    'headers': res.get('headers', {'Content-Type': 'text/plain'}),
                    'body': str(res.get('body', ''))
                }) + "\n")
            else:
                sys.stdout.write(json.dumps({
                    'status': 200,
                    'headers': {'Content-Type': 'text/plain'},
                    'body': str(res)
                }) + "\n")
            return None

        else:
            sys.stdout.write(json.dumps({
                'status': 500,
                'headers': {'Content-Type': 'text/plain'},
                'body': 'No app(environ, start_response) or fetch(req) found'
            }) + "\n")
            return None
    except Exception as e:
        import traceback
        sys.stdout.write(json.dumps({
            'status': 500,
            'headers': {'Content-Type': 'text/plain'},
            'body': traceback.format_exc()
        }) + "\n")
        return None
`

// Serve implements sandbox.ServeBackend using an in-process handler bridge over Wasm sessions.
func (b *Backend) Serve(ctx context.Context, req *sandbox.ServeRequest) (sandbox.GuestService, error) {
	if req.Pack == nil || req.Pack.Wasm == nil {
		return nil, fmt.Errorf("%w: pack %q has no wasm spec", sandbox.ErrUnsupported, req.Pack.Name)
	}
	if !req.Pack.Caps.ServeHandler {
		return nil, fmt.Errorf("%w: language %q does not support Wasm handler bridge (use Docker backend for socket servers)",
			sandbox.ErrUnsupported, req.Pack.Name)
	}

	// 1. Create long-lived session instance
	sessReq := &sandbox.SessionRequest{
		Pack: req.Pack,
		Config: sandbox.SessionConfig{
			ID:          fmt.Sprintf("serve-%s-%x", req.Pack.Name, time.Now().UnixNano()),
			Limits:      req.Limits,
			AutoRestart: true,
			IdleTimeout: 1 * time.Hour,
			MaxLifetime: 24 * time.Hour,
			Env:         req.Spec.Env,
		},
	}

	sess, err := b.NewSession(ctx, sessReq)
	if err != nil {
		return nil, fmt.Errorf("starting wasm serve session: %w", err)
	}

	// 2. Initialize user code and bridge adapter
	if req.Spec.Code != "" {
		initRes, err := sess.Eval(ctx, req.Spec.Code)
		if err != nil {
			_ = sess.Close()
			return nil, fmt.Errorf("initializing user code in wasm session: %w", err)
		}
		if initRes.ExitCode != 0 {
			_ = sess.Close()
			return nil, fmt.Errorf("user code initialization failed with exit code %d: %s", initRes.ExitCode, string(initRes.Stderr))
		}
	}

	// Install adapter bridge
	if req.Pack.Name == "js" || req.Pack.Name == "javascript" || req.Pack.Name == "quickjs" {
		if _, err := sess.Eval(ctx, jsServeBridge); err != nil {
			_ = sess.Close()
			return nil, fmt.Errorf("installing js serve bridge: %w", err)
		}
	} else if req.Pack.Name == "python" || req.Pack.Name == "py" || req.Pack.Name == "python3" {
		if _, err := sess.Eval(ctx, pyServeBridge); err != nil {
			_ = sess.Close()
			return nil, fmt.Errorf("installing python serve bridge: %w", err)
		}
	} else {
		_ = sess.Close()
		return nil, fmt.Errorf("%w: no handler bridge available for %q", sandbox.ErrUnsupported, req.Pack.Name)
	}

	waitCh := make(chan struct{})
	svc := &wasmGuestService{
		session: sess,
		waitCh:  waitCh,
	}

	// 3. Build http.Handler
	svc.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		reqMap := map[string]any{
			"method":  r.Method,
			"url":     r.URL.String(),
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			"headers": make(map[string]string),
			"body":    string(bodyBytes),
		}
		for k := range r.Header {
			reqMap["headers"].(map[string]string)[k] = r.Header.Get(k)
		}
		reqJSON, _ := json.Marshal(reqMap)

		var evalCmd string
		if req.Pack.Name == "js" || req.Pack.Name == "javascript" || req.Pack.Name == "quickjs" {
			evalCmd = fmt.Sprintf("__bridge_fetch(%s)", strconv.Quote(string(reqJSON)))
		} else {
			evalCmd = fmt.Sprintf("__bridge_http(%s)", strconv.Quote(string(reqJSON)))
		}

		evalCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		res, err := sess.Eval(evalCtx, evalCmd)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Wasm handler error: " + err.Error()))
			return
		}

		// Stream logs if logWriter provided
		if req.LogWriter != nil && len(res.Stderr) > 0 {
			_, _ = req.LogWriter.Write(res.Stderr)
		}

		raw := strings.TrimSpace(string(res.Stdout))
		var bridgeResp struct {
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}

		if err := json.Unmarshal([]byte(raw), &bridgeResp); err != nil {
			// Fallback: output was plain text
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(res.Stdout)
			return
		}

		for k, v := range bridgeResp.Headers {
			w.Header().Set(k, v)
		}
		status := bridgeResp.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(bridgeResp.Body))
	})

	// Monitor session liveness
	go func() {
		for {
			if !sess.Alive() {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		close(waitCh)
	}()

	return svc, nil
}

type wasmGuestService struct {
	session  sandbox.Session
	handler  http.Handler
	waitCh   chan struct{}
	outcome  sandbox.Outcome
	err      error
	mu       sync.RWMutex
	stopOnce sync.Once
}

func (s *wasmGuestService) PortMapping(guestPort int) (string, error) {
	return "", nil
}

func (s *wasmGuestService) Handler() http.Handler {
	return s.handler
}

func (s *wasmGuestService) Wait() (sandbox.Outcome, error) {
	<-s.waitCh
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.outcome, s.err
}

func (s *wasmGuestService) Stop() error {
	s.stopOnce.Do(func() {
		_ = s.session.Close()
	})
	return nil
}
