package serve

import (
	"net/http/httptest"
	"testing"
)

func TestRequestIsHTTPSOnlyTrustsLoopbackForwardedProto(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xfp        string
		want       bool
	}{
		{"loopback v4 with header", "127.0.0.1:52345", "https", true},
		{"loopback v6 with header", "[::1]:52345", "https", true},
		{"routable peer with header", "192.0.2.1:1234", "https", false},
		{"tailnet peer with header", "100.96.85.49:1234", "https", false},
		{"loopback without header", "127.0.0.1:52345", "", false},
		{"loopback with http header", "127.0.0.1:52345", "http", false},
		{"garbage remote addr", "not-an-addr", "https", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xfp != "" {
				r.Header.Set("X-Forwarded-Proto", tt.xfp)
			}
			if got := requestIsHTTPS(r); got != tt.want {
				t.Fatalf("requestIsHTTPS(remote=%s, xfp=%q) = %v, want %v", tt.remoteAddr, tt.xfp, got, tt.want)
			}
		})
	}
}
