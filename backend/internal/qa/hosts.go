// Package qa holds the pieces of browser QA that need no database: host rules, the filtering proxy,
// spec lint, scrubbing, result parsing, the QA stack and repo snapshots.
package qa

import (
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var hostRe = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// Loopback reports whether host is a loopback name or address.
func Loopback(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// NormalizeHost validates one allowed-host entry ("example.test" or "*.example.test") and lower-cases it.
func NormalizeHost(s string) (string, bool) {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	if s == "" || len(s) > 253 || !hostRe.MatchString(s) {
		return "", false
	}
	return s, true
}

// HostAllowed reports whether host (a bare host or host:port) may be reached. Loopback is always allowed
// (it is the QA stack itself); everything else must match an entry exactly or by "*.suffix".
func HostAllowed(host string, allowed []string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(strings.ToLower(host), "[]"), ".")
	if host == "" {
		return false
	}
	if Loopback(host) {
		return true
	}
	for _, a := range allowed {
		a = strings.ToLower(a)
		if a == host {
			return true
		}
		if strings.HasPrefix(a, "*.") && strings.HasSuffix(host, a[1:]) && len(host) > len(a)-1 {
			return true
		}
	}
	return false
}

// Proxy is a tiny forward proxy that only lets allowed hosts through. Chromium is pointed at it for the
// lifetime of one QA run, so IP-literal URLs and WebSockets are covered too. Refusals are recorded.
type Proxy struct {
	ln      net.Listener
	srv     *http.Server
	allowed []string
	mu      sync.Mutex
	blocked map[string]bool
}

// NewProxy listens on a random loopback port.
func NewProxy(allowed []string) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{ln: ln, allowed: allowed, blocked: map[string]bool{}}
	p.srv = &http.Server{Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: 10 * time.Second}
	go p.srv.Serve(ln)
	return p, nil
}

func (p *Proxy) Addr() string { return p.ln.Addr().String() }
func (p *Proxy) Close()       { p.srv.Close() }

// Blocked lists the hosts that were refused, sorted.
func (p *Proxy) Blocked() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.blocked))
	for h := range p.blocked {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func (p *Proxy) refuse(w http.ResponseWriter, host string) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	p.mu.Lock()
	p.blocked[strings.ToLower(host)] = true
	p.mu.Unlock()
	http.Error(w, "host is not in the QA allowed list", http.StatusForbidden)
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		if !HostAllowed(r.Host, p.allowed) {
			p.refuse(w, r.Host)
			return
		}
		dst, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			dst.Close()
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		c, _, err := hj.Hijack()
		if err != nil {
			dst.Close()
			return
		}
		c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { io.Copy(dst, c); dst.Close() }()
		go func() { io.Copy(c, dst); c.Close() }()
		return
	}
	if !r.URL.IsAbs() || !HostAllowed(r.URL.Host, p.allowed) {
		p.refuse(w, r.URL.Host)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	resp, err := (&http.Transport{Proxy: nil}).RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
