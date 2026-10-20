package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// IPResolver определяет адрес клиента. X-Forwarded-For учитывается, только если соединение
// пришло от доверенного прокси (TRUSTED_PROXIES): иначе любой клиент подделал бы заголовок
// и обошёл лимиты. Из цепочки берётся самый правый адрес, который не доверенный прокси, —
// его добавил наш прокси, а левее мог написать сам клиент.
type IPResolver struct {
	trusted []netip.Prefix
}

// NewIPResolver — trusted пустой: заголовок не учитывается никогда.
func NewIPResolver(trusted []netip.Prefix) *IPResolver {
	return &IPResolver{trusted: trusted}
}

// ClientIP — адрес клиента запроса.
func (r *IPResolver) ClientIP(req *http.Request) string {
	remote, ok := parseRemote(req.RemoteAddr)
	if !ok {
		return req.RemoteAddr
	}
	if r == nil || !r.isTrusted(remote) {
		return remote.String()
	}
	var chain []string
	for _, h := range req.Header.Values("X-Forwarded-For") {
		chain = append(chain, strings.Split(h, ",")...)
	}
	client := remote
	for i := len(chain) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			break // мусор в цепочке — дальше не верим, остаёмся на последнем проверенном адресе
		}
		client = a.Unmap()
		if !r.isTrusted(client) {
			return client.String()
		}
	}
	return client.String()
}

func (r *IPResolver) isTrusted(a netip.Addr) bool {
	for _, p := range r.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func parseRemote(s string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		host = s
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}
