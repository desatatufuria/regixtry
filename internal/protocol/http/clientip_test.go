package regixtryhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	t.Parallel()

	got, err := ParseTrustedProxies(" 192.168.1.245 , 10.0.0.0/8,fd00::/8 ,, ")
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%v)", len(got), got)
	}
	if empty, err := ParseTrustedProxies("  "); err != nil || len(empty) != 0 {
		t.Fatalf("empty list = %v, %v; want none", empty, err)
	}
	for _, bad := range []string{"not-an-ip", "10.0.0.0/33", "10.0.0.1/8x"} {
		if _, err := ParseTrustedProxies(bad); err == nil {
			t.Fatalf("ParseTrustedProxies(%q) accepted an invalid entry", bad)
		}
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	trusted, err := ParseTrustedProxies("192.168.1.245,10.0.0.0/8,fd00::/8")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		trusted bool
		remote  string
		xff     []string
		want    string
	}{
		{"no proxy configured uses RemoteAddr", false, "203.0.113.7:51000", nil, "203.0.113.7"},
		{"forged XFF ignored without trusted proxy", false, "203.0.113.7:51000", []string{"6.6.6.6"}, "203.0.113.7"},
		{"forged XFF ignored when peer is not a trusted proxy", true, "203.0.113.7:51000", []string{"6.6.6.6"}, "203.0.113.7"},
		{"trusted peer, single client", true, "192.168.1.245:40000", []string{"198.51.100.4"}, "198.51.100.4"},
		{"client-forged first value is skipped, rightmost untrusted wins", true, "192.168.1.245:40000", []string{"6.6.6.6, 198.51.100.4"}, "198.51.100.4"},
		{"trusted hops on the right are skipped", true, "10.1.1.1:40000", []string{"198.51.100.4, 10.2.2.2, 10.3.3.3"}, "198.51.100.4"},
		{"multiple header lines are merged in order", true, "192.168.1.245:40000", []string{"6.6.6.6", "198.51.100.4"}, "198.51.100.4"},
		{"multiple header lines with spaces and commas", true, "192.168.1.245:40000", []string{"6.6.6.6, 7.7.7.7 ", " 198.51.100.4,10.9.9.9"}, "198.51.100.4"},
		{"long chain only trusts the tail", true, "10.0.0.1:1", []string{"1.1.1.1,2.2.2.2,3.3.3.3,4.4.4.4,5.5.5.5,6.6.6.6,10.0.0.2,10.0.0.3"}, "6.6.6.6"},
		{"every hop trusted falls back to the leftmost", true, "10.0.0.1:1", []string{"10.0.0.5, 10.0.0.6"}, "10.0.0.5"},
		{"trusted peer without XFF is the peer", true, "192.168.1.245:40000", nil, "192.168.1.245"},
		{"garbage entry stops the walk at the peer-side value", true, "10.0.0.1:1", []string{"198.51.100.4, garbage"}, "10.0.0.1"},
		{"IPv6 RemoteAddr with port", false, "[2001:db8::1]:443", nil, "2001:db8::1"},
		{"IPv6 client behind trusted IPv6 proxy", true, "[fd00::1]:443", []string{"2001:db8::9"}, "2001:db8::9"},
		{"IPv4-mapped IPv6 peer matches the IPv4 trust entry", true, "[::ffff:192.168.1.245]:40000", []string{"198.51.100.4"}, "198.51.100.4"},
		{"XFF entry with port is understood", true, "192.168.1.245:40000", []string{"198.51.100.4:5555"}, "198.51.100.4"},
		{"bracketed IPv6 XFF entry", true, "192.168.1.245:40000", []string{"[2001:db8::9]"}, "2001:db8::9"},
		{"RemoteAddr without port", false, "203.0.113.7", nil, "203.0.113.7"},
		{"unparseable RemoteAddr is reported as unknown", false, "@", nil, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
			req.RemoteAddr = tc.remote
			for _, line := range tc.xff {
				req.Header.Add("X-Forwarded-For", line)
			}
			req.Header.Set("X-Real-IP", "9.9.9.9") // never trusted
			var list = trusted
			if !tc.trusted {
				list = nil
			}
			if got := clientIP(req, list); got != tc.want {
				t.Fatalf("clientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
