package httpapi

import (
	"net"
	"net/http"
	"strings"
)

// TrustedProxies rewrites RemoteAddr from X-Forwarded-For only when the
// immediate peer is in the trusted CIDR list. Empty list → ignore forwarded headers.
func TrustedProxies(cidrs []string) func(http.Handler) http.Handler {
	nets := parseCIDRs(cidrs)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(nets) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := net.ParseIP(host)
			if ip == nil || !ipInNets(ip, nets) {
				next.ServeHTTP(w, r)
				return
			}
			xff := r.Header.Get("X-Forwarded-For")
			if xff == "" {
				next.ServeHTTP(w, r)
				return
			}
			// Left-most is the original client in standard proxy chains.
			parts := strings.Split(xff, ",")
			client := strings.TrimSpace(parts[0])
			if parsed := net.ParseIP(client); parsed != nil {
				r.RemoteAddr = net.JoinHostPort(client, "0")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			// Allow bare IP as /32 or /128.
			if ip := net.ParseIP(c); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			}
			continue
		}
		out = append(out, n)
	}
	return out
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
