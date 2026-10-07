package netpolicy

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrSSRF means the request targets a private, loopback, link-local or metadata address.
var ErrSSRF = errors.New("netpolicy: SSRF violation")

// ErrHostNotAllowed means the host is not in the allow list.
var ErrHostNotAllowed = errors.New("netpolicy: host not allowed")

// ErrPortNotAllowed means the destination port is not in the allow list.
var ErrPortNotAllowed = errors.New("netpolicy: port not allowed")

// ErrMaxRequestsExceeded means the request count limit was hit.
var ErrMaxRequestsExceeded = errors.New("netpolicy: max requests exceeded")

// ErrMaxBytesExceeded means the total bytes limit was hit.
var ErrMaxBytesExceeded = errors.New("netpolicy: max bytes exceeded")

// ErrTimeout means the request timed out.
var ErrTimeout = errors.New("netpolicy: request timeout")

// HostMatcher matches host names, supporting exact and wildcard ("*.example.com") patterns.
type HostMatcher struct {
	patterns []string
}

// NewHostMatcher creates a matcher from a list of patterns.
func NewHostMatcher(patterns []string) *HostMatcher {
	return &HostMatcher{patterns: patterns}
}

// Matches returns true if host matches any pattern.
func (hm *HostMatcher) Matches(host string) bool {
	for _, p := range hm.patterns {
		if p == host {
			return true
		}
		if strings.HasPrefix(p, "*.") && len(p) > 2 {
			suffix := p[2:]
			if host == suffix {
				return true
			}
			if strings.HasSuffix(host, "."+suffix) {
				prefix := host[:len(host)-len("."+suffix)]
				if !strings.Contains(prefix, ".") {
					return true
				}
			}
		}
	}
	return false
}

// IPClassifier classifies IP addresses into categories.
type IPClassifier struct {
	privateBlocks []*net.IPNet
	loopbackBlock *net.IPNet
	linkLocalBlock *net.IPNet
	metadataBlock  *net.IPNet
}

func initIPClassifier() *IPClassifier {
	privateBlocks := []*net.IPNet{}
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		_, block, _ := net.ParseCIDR(cidr)
		privateBlocks = append(privateBlocks, block)
	}
	_, loopback, _ := net.ParseCIDR("127.0.0.0/8")
	_, linkLocal, _ := net.ParseCIDR("169.254.0.0/16")
	_, metadata, _ := net.ParseCIDR("169.254.169.254/32")
	return &IPClassifier{
		privateBlocks: privateBlocks,
		loopbackBlock: loopback,
		linkLocalBlock: linkLocal,
		metadataBlock:  metadata,
	}
}

var classifier = initIPClassifier()

// Classify returns the category of ip.
type Category int

const (
	CategoryPublic Category = iota
	CategoryPrivate
	CategoryLoopback
	CategoryLinkLocal
	CategoryMetadata
)

func (c *IPClassifier) Classify(ip net.IP) Category {
	if c.metadataBlock.Contains(ip) {
		return CategoryMetadata
	}
	if c.loopbackBlock.Contains(ip) {
		return CategoryLoopback
	}
	if c.linkLocalBlock.Contains(ip) {
		return CategoryLinkLocal
	}
	for _, block := range c.privateBlocks {
		if block.Contains(ip) {
			return CategoryPrivate
		}
	}
	return CategoryPublic
}

// ResolveThenPin resolves a host to an IP, validates it against the allow list,
// and pins the resolved IP for the duration of the request to prevent DNS rebinding.
// It returns the pinned IP and an error if validation fails.
func (hm *HostMatcher) ResolveThenPin(host string, ports []int, allowPrivate bool) (net.IP, error) {
	// Resolve host to IP (IPv4 only for simplicity)
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("netpolicy: host resolution failed")
	}
	// Use first IPv4 address
	var ip net.IP
	for _, candidate := range ips {
		if candidate.To4() != nil {
			ip = candidate
			break
		}
	}
	if ip == nil {
		return nil, errors.New("netpolicy: no IPv4 address found")
	}

	// Classify IP
	cat := classifier.Classify(ip)
	if cat == CategoryLoopback || cat == CategoryLinkLocal || cat == CategoryMetadata {
		return nil, ErrSSRF
	}
	if cat == CategoryPrivate && !allowPrivate {
		return nil, ErrSSRF
	}

	// Check host pattern
	if !hm.Matches(host) {
		return nil, ErrHostNotAllowed
	}

	// Check port
	if len(ports) == 0 {
		ports = []int{80, 443}
	}
	allowed := false
	for _, p := range ports {
		if p == 0 { // wildcard port
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrPortNotAllowed
	}

	return ip, nil
}

// RedirectValidator validates redirects to ensure they don't target private/internal IPs.
type RedirectValidator struct {
	allowPrivate bool
	matcher     *HostMatcher
}

func NewRedirectValidator(allowPrivate bool, patterns []string) *RedirectValidator {
	return &RedirectValidator{allowPrivate: allowPrivate, matcher: NewHostMatcher(patterns)}
}

// ValidateRedirect checks if a redirect target is allowed.
func (rv *RedirectValidator) ValidateRedirect(target string) error {
	// Parse target host and port
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	// Resolve target host
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return errors.New("netpolicy: redirect host resolution failed")
	}
	var ip net.IP
	for _, candidate := range ips {
		if candidate.To4() != nil {
			ip = candidate
			break
		}
	}
	if ip == nil {
		return errors.New("netpolicy: no IPv4 address found for redirect")
	}

	// Classify IP
	cat := classifier.Classify(ip)
	if cat == CategoryLoopback || cat == CategoryLinkLocal || cat == CategoryMetadata {
		return ErrSSRF
	}
	if cat == CategoryPrivate && !rv.allowPrivate {
		return ErrSSRF
	}

	// Check host pattern
	if !rv.matcher.Matches(host) {
		return ErrHostNotAllowed
	}

	return nil
}

// RequestLimiter tracks request count and total bytes.
type RequestLimiter struct {
	mu            sync.Mutex
	requests      int
	maxRequests   int
	bytes         uint64
	maxBytes      uint64
	timeout       time.Duration
	lastReset     time.Time
}

func NewRequestLimiter(maxRequests int, maxBytes uint64, timeout time.Duration) *RequestLimiter {
	return &RequestLimiter{maxRequests: maxRequests, maxBytes: maxBytes, timeout: timeout, lastReset: time.Now()}
}

// Acquire reserves a request slot and checks limits.
func (rl *RequestLimiter) Acquire() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if time.Since(rl.lastReset) >= rl.timeout {
		rl.lastReset = time.Now()
		rl.requests = 0
		rl.bytes = 0
	}
	if rl.maxRequests > 0 && rl.requests >= rl.maxRequests {
		return ErrMaxRequestsExceeded
	}
	rl.requests++
	return nil
}

// RecordBytes records the number of bytes transferred.
func (rl *RequestLimiter) RecordBytes(n int) error {
	if rl.maxBytes == 0 {
		return nil
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.bytes+uint64(n) > rl.maxBytes {
		return ErrMaxBytesExceeded
	}
	rl.bytes += uint64(n)
	return nil
}

// HTTPClient is a custom HTTP client that enforces netpolicy.
type HTTPClient struct {
	matcher       *HostMatcher
	ports         []int
	allowPrivate  bool
	limiter       *RequestLimiter
	redirectValidator *RedirectValidator
	httpClient   *http.Client
}

func NewHTTPClient(matcher *HostMatcher, ports []int, allowPrivate bool, limiter *RequestLimiter, redirectValidator *RedirectValidator) *HTTPClient {
	return &HTTPClient{
		matcher: matcher,
		ports: ports,
		allowPrivate: allowPrivate,
		limiter: limiter,
		redirectValidator: redirectValidator,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Validate redirect target
				if err := redirectValidator.ValidateRedirect(req.URL.Host); err != nil {
					return err
				}
				return nil
			},
		},
	}
}

// Do performs an HTTP request with netpolicy checks.
func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Acquire request slot
	if err := c.limiter.Acquire(); err != nil {
		return nil, err
	}

	// Resolve host and validate
	host := req.URL.Hostname()
	if host == "" {
		host = req.URL.Host
	}
	ip, err := c.matcher.ResolveThenPin(host, c.ports, c.allowPrivate)
	if err != nil {
		return nil, err
	}

	// Override request URL with pinned IP
	req.URL.Host = ip.String() + ":" + req.URL.Port()

	// Perform request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// Record bytes
	if c.limiter != nil {
		if resp.ContentLength > 0 {
			c.limiter.RecordBytes(int(resp.ContentLength))
		}
	}

	return resp, nil
}