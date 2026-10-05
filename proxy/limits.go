package proxy

import "time"

// Limits configures security and resource boundaries for the proxy.
type Limits struct {
	// MaxConnections is the maximum number of concurrent client connections.
	// If 0, a sensible default (256) is applied.
	MaxConnections int

	// MaxRequestBody is the maximum allowed size of an HTTP request body in bytes.
	// Defaults to 10MB.
	MaxRequestBody int64

	// MaxHeaderBytes is the maximum allowed size of HTTP request headers.
	// Defaults to 1MB.
	MaxHeaderBytes int

	// ReadHeaderTimeout is the amount of time allowed to read request headers.
	// Protects against Slowloris attacks. Defaults to 5s.
	ReadHeaderTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	// when keep-alive is enabled. Defaults to 30s.
	IdleTimeout time.Duration

	// ReadTimeout is the maximum duration for reading the entire request.
	// Defaults to 30s.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response.
	// Defaults to 30s.
	WriteTimeout time.Duration
}

// DefaultLimits returns the baseline secure proxy limits.
func DefaultLimits() Limits {
	return Limits{
		MaxConnections:    256,
		MaxRequestBody:    10 * 1024 * 1024, // 10 MB
		MaxHeaderBytes:    1 * 1024 * 1024,  // 1 MB
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
}

// resolveLimits fills in zero-valued fields with default values.
func resolveLimits(l Limits) Limits {
	d := DefaultLimits()
	if l.MaxConnections <= 0 {
		l.MaxConnections = d.MaxConnections
	}
	if l.MaxRequestBody <= 0 {
		l.MaxRequestBody = d.MaxRequestBody
	}
	if l.MaxHeaderBytes <= 0 {
		l.MaxHeaderBytes = d.MaxHeaderBytes
	}
	if l.ReadHeaderTimeout <= 0 {
		l.ReadHeaderTimeout = d.ReadHeaderTimeout
	}
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = d.IdleTimeout
	}
	if l.ReadTimeout <= 0 {
		l.ReadTimeout = d.ReadTimeout
	}
	if l.WriteTimeout <= 0 {
		l.WriteTimeout = d.WriteTimeout
	}
	return l
}
