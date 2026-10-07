package netpolicy

import (
	"net"
	"testing"
	"time"
)

func TestHostMatcher(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		host    string
		want    bool
	}{
		{"exact", "example.com", "example.com", true},
		{"exact fail", "example.com", "other.com", false},
		{"wildcard subdomain", "*.example.com", "sub.example.com", true},
		{"wildcard root", "*.example.com", "example.com", true},
		{"wildcard fail", "*.example.com", "sub.other.com", false},
		{"wildcard fail", "*.example.com", "sub.example.org", false},
		{"no dot in host", "*.example.com", "sub", false},
		{"empty pattern", "", "example.com", false},
		{"wildcard single label", "*.com", "example.com", true},
		{"wildcard single label fail", "*.com", "net.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := NewHostMatcher([]string{tt.pattern})
			got := hm.Matches(tt.host)
			if got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestIPClassifier(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want Category
	}{
		{"loopback", "127.0.0.1", CategoryLoopback},
		{"loopback range", "127.255.255.255", CategoryLoopback},
		{"private 10", "10.0.0.1", CategoryPrivate},
		{"private 172", "172.16.0.1", CategoryPrivate},
		{"private 192", "192.168.1.1", CategoryPrivate},
		{"link local", "169.254.1.1", CategoryLinkLocal},
		{"metadata", "169.254.169.254", CategoryMetadata},
		{"public", "8.8.8.8", CategoryPublic},
		{"public ipv6", "2001:4860:4860::8888", CategoryPublic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := classifier.Classify(ip)
			if got != tt.want {
				t.Errorf("Classify(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestResolveThenPin(t *testing.T) {
	// Test that ResolveThenPin correctly handles valid and invalid hosts
	hm := NewHostMatcher([]string{"example.com", "*.example.com"})

	// Test with a non-existent host
	_, err := hm.ResolveThenPin("nonexistent.invalid", []int{443}, false)
	if err == nil {
		t.Error("expected error for nonexistent host")
	}

	// Test with a host that doesn't match the pattern
	_, err = hm.ResolveThenPin("other.com", []int{443}, false)
	if err == nil {
		t.Error("expected error for non-matching host")
	}

	// Test with a private IP host
	_, err = hm.ResolveThenPin("127.0.0.1", []int{443}, false)
	if err == nil {
		t.Error("expected error for loopback host")
	}

	// Test with allowPrivate
	_, err = hm.ResolveThenPin("127.0.0.1", []int{443}, true)
	if err == nil {
		t.Error("expected error for loopback host even with allowPrivate")
	}
}

func TestRedirectValidator(t *testing.T) {
	rv := NewRedirectValidator(false, []string{"example.com", "*.example.com"})

	// Test valid redirect
	err := rv.ValidateRedirect("example.com:80")
	if err != nil {
		t.Errorf("expected no error for valid redirect, got %v", err)
	}

	// Test invalid redirect to loopback
	err = rv.ValidateRedirect("127.0.0.1:80")
	if err == nil {
		t.Error("expected error for loopback redirect")
	}

	// Test invalid redirect to non-matching host
	err = rv.ValidateRedirect("other.com:80")
	if err == nil {
		t.Error("expected error for non-matching host redirect")
	}
}

func TestRequestLimiter(t *testing.T) {
	rl := NewRequestLimiter(2, 100, time.Second)

	// Acquire first request
	err := rl.Acquire()
	if err != nil {
		t.Errorf("first acquire failed: %v", err)
	}

	// Acquire second request
	err = rl.Acquire()
	if err != nil {
		t.Errorf("second acquire failed: %v", err)
	}

	// Third request should fail (max 2)
	err = rl.Acquire()
	if err != ErrMaxRequestsExceeded {
		t.Errorf("expected ErrMaxRequestsExceeded, got %v", err)
	}

	// Test bytes limit
	rl2 := NewRequestLimiter(0, 100, time.Second)
	err = rl2.RecordBytes(50)
	if err != nil {
		t.Errorf("first record bytes failed: %v", err)
	}
	err = rl2.RecordBytes(60)
	if err != ErrMaxBytesExceeded {
		t.Errorf("expected ErrMaxBytesExceeded, got %v", err)
	}
}