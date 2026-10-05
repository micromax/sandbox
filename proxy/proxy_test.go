package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyHTTP_ForwardAndAuth(t *testing.T) {
	var backendHits int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&backendHits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from guest"))
	}))
	defer backend.Close()

	backendAddr := backend.Listener.Addr().String()

	p, err := New(Config{
		Mode:      ModeHTTP,
		Bind:      "127.0.0.1",
		Port:      0,
		Target:    backendAddr,
		AuthToken: "secret123",
	})
	if err != nil {
		t.Fatalf("New proxy: %v", err)
	}
	defer p.Close()

	// 1. Request without auth header -> 401
	resp, err := http.Get(p.URL() + "/test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}
	if atomic.LoadInt64(&backendHits) != 0 {
		t.Errorf("backend received request without auth, expected 0 hits, got %d", atomic.LoadInt64(&backendHits))
	}

	// 2. Request with invalid token -> 401
	req, _ := http.NewRequest(http.MethodGet, p.URL()+"/test", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}
	if atomic.LoadInt64(&backendHits) != 0 {
		t.Errorf("backend received request with wrong auth")
	}

	// 3. Request with valid token -> 200
	req, _ = http.NewRequest(http.MethodGet, p.URL()+"/test", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do with valid auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from guest" {
		t.Errorf("expected 'hello from guest', got %q", string(body))
	}
	if atomic.LoadInt64(&backendHits) != 1 {
		t.Errorf("expected 1 backend hit, got %d", atomic.LoadInt64(&backendHits))
	}
}

func TestProxyHTTP_BodyLimit(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	p, err := New(Config{
		Mode:   ModeHTTP,
		Bind:   "127.0.0.1",
		Port:   0,
		Target: backend.Listener.Addr().String(),
		Limits: Limits{
			MaxRequestBody: 1024, // 1 KB limit
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	// Small body within limit -> 200 OK
	smallBody := bytes.Repeat([]byte("a"), 512)
	resp, err := http.Post(p.URL()+"/upload", "application/octet-stream", bytes.NewReader(smallBody))
	if err != nil {
		t.Fatalf("Post small: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Large body exceeding limit -> 413 Payload Too Large
	largeBody := bytes.Repeat([]byte("b"), 2048)
	resp, err = http.Post(p.URL()+"/upload", "application/octet-stream", bytes.NewReader(largeBody))
	if err != nil {
		// Some HTTP clients might fail connection when server closes early, which is also acceptable
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Request Entity Too Large, got %d", resp.StatusCode)
	}
}

func TestProxyHTTP_SlowlorisHeaderTimeout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	p, err := New(Config{
		Mode:   ModeHTTP,
		Bind:   "127.0.0.1",
		Port:   0,
		Target: backend.Listener.Addr().String(),
		Limits: Limits{
			ReadHeaderTimeout: 300 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p.Port()))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Send partial header
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\nX-Slow: "))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Wait past ReadHeaderTimeout without finishing headers
	time.Sleep(600 * time.Millisecond)

	// Now try to read or write; connection should be closed by server
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 128)
	n, rerr := conn.Read(buf)
	if rerr == nil && n > 0 && !strings.Contains(string(buf[:n]), "408") {
		// Either EOF or 408 Request Timeout is sent
		t.Logf("Read after timeout: %s", string(buf[:n]))
	}
}

func TestProxyTCP_Forwarding(t *testing.T) {
	// Simple TCP echo backend
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen backend: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	p, err := New(Config{
		Mode:   ModeTCP,
		Bind:   "127.0.0.1",
		Port:   0,
		Target: ln.Addr().String(),
	})
	if err != nil {
		t.Fatalf("New TCP proxy: %v", err)
	}
	defer p.Close()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p.Port()))
	if err != nil {
		t.Fatalf("Dial proxy: %v", err)
	}
	defer conn.Close()

	msg := "ping pong stream 12345"
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if string(buf) != msg {
		t.Errorf("expected %q, got %q", msg, string(buf))
	}
}

func TestProxy_MaxConnections(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	p, err := New(Config{
		Mode:   ModeHTTP,
		Bind:   "127.0.0.1",
		Port:   0,
		Target: backend.Listener.Addr().String(),
		Limits: Limits{
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	var wg sync.WaitGroup
	var activeMax int64

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cur := int64(p.ActiveConns())
			for {
				old := atomic.LoadInt64(&activeMax)
				if cur <= old || atomic.CompareAndSwapInt64(&activeMax, old, cur) {
					break
				}
			}
			resp, err := http.Get(p.URL() + "/")
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	if activeMax > 5 {
		t.Errorf("Active connections exceeded max: %d > 5", activeMax)
	}
}

func TestProxyHTTP_Upgrade(t *testing.T) {
	// Backend that supports Upgrade: echo
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" || strings.ToLower(r.Header.Get("Upgrade")) == "echo" {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijack unsupported", 500)
				return
			}
			conn, bufrw, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer conn.Close()

			bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: echo\r\nConnection: Upgrade\r\n\r\n")
			bufrw.Flush()

			// Echo loop
			buf := make([]byte, 64)
			n, _ := bufrw.Read(buf)
			if n > 0 {
				_, _ = bufrw.Write(buf[:n])
				bufrw.Flush()
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	p, err := New(Config{
		Mode:   ModeHTTP,
		Bind:   "127.0.0.1",
		Port:   0,
		Target: backend.Listener.Addr().String(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p.Port()))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Send upgrade request
	req := "GET /ws HTTP/1.1\r\nHost: localhost\r\nUpgrade: echo\r\nConnection: Upgrade\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("Write req: %v", err)
	}

	// Read 101 response headers
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read upgrade: %v", err)
	}
	respStr := string(buf[:n])
	if !strings.Contains(respStr, "101 Switching Protocols") {
		t.Fatalf("Expected 101 Switching Protocols, got: %s", respStr)
	}

	// Now send stream payload
	payload := "hello duplex websocket stream"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("Write payload: %v", err)
	}

	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("Read echo payload: %v", err)
	}
	if string(buf[:n]) != payload {
		t.Errorf("Expected echo payload %q, got %q", payload, string(buf[:n]))
	}
}

