package httpx

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.30.0.0/24"), netip.MustParsePrefix("10.0.0.1/32")}
	tests := []struct {
		name    string
		remote  string
		xff     []string
		trusted bool // резолвер с доверенными прокси
		want    string
	}{
		{"без прокси", "203.0.113.5:4000", nil, true, "203.0.113.5"},
		{"клиент подделал XFF напрямую", "203.0.113.5:4000", []string{"1.2.3.4"}, true, "203.0.113.5"},
		{"через доверенный прокси", "172.30.0.2:5000", []string{"198.51.100.7"}, true, "198.51.100.7"},
		{"клиент дописал адрес слева", "172.30.0.2:5000", []string{"1.2.3.4, 198.51.100.7"}, true, "198.51.100.7"},
		{"цепочка из двух доверенных", "10.0.0.1:5000", []string{"198.51.100.7, 172.30.0.9"}, true, "198.51.100.7"},
		{"несколько заголовков", "172.30.0.2:5000", []string{"1.2.3.4", "198.51.100.7"}, true, "198.51.100.7"},
		{"доверенный без XFF", "172.30.0.2:5000", nil, true, "172.30.0.2"},
		{"мусор в цепочке", "172.30.0.2:5000", []string{"198.51.100.7, not-an-ip"}, true, "172.30.0.2"},
		{"все адреса доверенные", "172.30.0.2:5000", []string{"172.30.0.3"}, true, "172.30.0.3"},
		{"IPv6 клиент", "172.30.0.2:5000", []string{"2001:db8::1"}, true, "2001:db8::1"},
		{"IPv4 в IPv6", "[::ffff:172.30.0.2]:5000", []string{"198.51.100.7"}, true, "198.51.100.7"},
		{"доверенных нет — заголовок не читаем", "172.30.0.2:5000", []string{"198.51.100.7"}, false, "172.30.0.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			res := NewIPResolver(nil)
			if tt.trusted {
				res = NewIPResolver(trusted)
			}
			if got := res.ClientIP(r); got != tt.want {
				t.Errorf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
