package proxy

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func (p *Proxy) startHTTP() error {
	var targetHandler http.Handler

	if p.cfg.TargetHandler != nil {
		targetHandler = p.cfg.TargetHandler
	} else {
		targetURL, err := url.Parse("http://" + p.cfg.Target)
		if err != nil {
			return err
		}
		rp := httputil.NewSingleHostReverseProxy(targetURL)
		rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_, _ = w.Write([]byte("413 Request Entity Too Large\n"))
				return
			}
			// Backend is unreachable or connection dropped
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("502 Bad Gateway: guest service unreachable\n"))
		}
		targetHandler = rp
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Enforce Bearer token auth if configured
		if p.cfg.AuthToken != "" {
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + p.cfg.AuthToken
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expected)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="sandbox"`)
				http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// 2. Enforce request body limits
		if p.cfg.Limits.MaxRequestBody > 0 && r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, p.cfg.Limits.MaxRequestBody)
		}

		targetHandler.ServeHTTP(w, r)
	})

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: p.cfg.Limits.ReadHeaderTimeout,
		IdleTimeout:       p.cfg.Limits.IdleTimeout,
		ReadTimeout:       p.cfg.Limits.ReadTimeout,
		WriteTimeout:      p.cfg.Limits.WriteTimeout,
		MaxHeaderBytes:    p.cfg.Limits.MaxHeaderBytes,
	}

	p.httpServer = server

	go func() {
		// Serve handles requests on the limitListener
		_ = server.Serve(p.listener)
	}()

	return nil
}
