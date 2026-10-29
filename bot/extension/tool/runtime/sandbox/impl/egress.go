package impl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nekocode/bot/extension/tool/runtime/netpolicy"
)

// Each invocation owns its proxy, policy, listeners and live tunnels.
var createEgressProxy = startEgressProxy

type egressProxy struct {
	server    *http.Server
	transport *http.Transport
	listener  net.Listener
	dir       string
	mu        sync.Mutex
	conns     map[net.Conn]bool
	closed    bool
	once      sync.Once
}

type egressConnKey struct{}
type trackedEgressConn struct {
	net.Conn
	owner      *egressProxy
	reader     *bufio.Reader
	prefix     []byte
	rawHost    string
	headerRead bool
}

// Go's HTTP proxy parser replaces Host with the absolute URL's authority.
// Preserve the original Host before parsing, for an independent consistency check.
func (c *trackedEgressConn) Read(b []byte) (int, error) {
	if !c.headerRead {
		c.headerRead = true
		c.reader = bufio.NewReaderSize(c.Conn, 64<<10)
		for {
			rawLine, err := c.reader.ReadSlice('\n')
			line := string(rawLine)
			if err != nil {
				return 0, err
			}
			if len(c.prefix)+len(line) > 64<<10 {
				return 0, fmt.Errorf("proxy header too large")
			}
			c.prefix = append(c.prefix, line...)
			if line == "\r\n" {
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(key, "Host") {
				c.rawHost = strings.TrimSpace(value)
			}
		}
	}
	if len(c.prefix) > 0 {
		n := copy(b, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.reader.Read(b)
}

func (c *trackedEgressConn) Close() error {
	err := c.Conn.Close()
	c.owner.mu.Lock()
	delete(c.owner.conns, c)
	c.owner.mu.Unlock()
	return err
}

type trackedEgressListener struct {
	net.Listener
	owner *egressProxy
}

func (l trackedEgressListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.owner.mu.Lock()
		if l.owner.closed || len(l.owner.conns) >= 128 {
			l.owner.mu.Unlock()
			c.Close()
			continue
		}
		t := &trackedEgressConn{Conn: c, owner: l.owner}
		l.owner.conns[t] = true
		l.owner.mu.Unlock()
		return t, nil
	}
}

func startEgressProxy(domains []string) (*egressProxy, error) {
	p := &egressProxy{conns: map[net.Conn]bool{}}
	var err error
	p.dir, err = os.MkdirTemp("", "nekocode-egress-")
	if err != nil {
		return nil, err
	}
	p.listener, err = net.Listen("unix", filepath.Join(p.dir, "proxy.sock"))
	if err != nil {
		os.RemoveAll(p.dir)
		return nil, err
	}
	allowed := netpolicy.Intersect(nil, domains)
	p.transport = &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialEgress(ctx, address, allowed, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)
	}, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second}
	p.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10, ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		return context.WithValue(ctx, egressConnKey{}, c.(*trackedEgressConn))
	}, Handler: http.HandlerFunc(p.serve)}
	go func() { _ = p.server.Serve(trackedEgressListener{Listener: p.listener, owner: p}) }()
	return p, nil
}
func (p *egressProxy) socket() string { return filepath.Join(p.dir, "proxy.sock") }
func (p *egressProxy) Close() {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		connections := make([]net.Conn, 0, len(p.conns))
		for c := range p.conns {
			connections = append(connections, c)
		}
		p.mu.Unlock()
		_ = p.server.Close()
		for _, c := range connections {
			_ = c.Close()
		}
		p.transport.CloseIdleConnections()
		_ = os.RemoveAll(p.dir)
	})
}

func publicEgressAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "2001::/32", "2002::/16", "fec0::/10"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}

type egressDenial struct{ message string }

func (e *egressDenial) Error() string { return e.message }
func denyEgress(format string, args ...any) error {
	return &egressDenial{message: fmt.Sprintf(format, args...)}
}

type egressLookup func(context.Context, string, string) ([]netip.Addr, error)
type egressDial func(context.Context, string, string) (net.Conn, error)

func dialEgress(ctx context.Context, address string, allowed []string, lookup egressLookup, dial egressDial) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (port != "80" && port != "443") {
		return nil, denyEgress("egress denied: invalid target or port")
	}
	domains, err := netpolicy.Normalize([]string{host})
	if err != nil {
		return nil, denyEgress("egress denied: target must be a DNS domain")
	}
	host = domains[0]
	if !netpolicy.Covers(allowed, []string{host}) {
		return nil, denyEgress("egress denied for %s:%s: domain not authorized", host, port)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, denyEgress("egress denied for %s:%s: DNS lookup failed", host, port)
	}
	for _, ip := range ips {
		if !publicEgressAddress(ip) {
			return nil, denyEgress("egress denied for %s:%s: non-public address", host, port)
		}
	}
	for _, ip := range ips {
		conn, err := dial(ctx, "tcp", net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, denyEgress("egress connection failed for %s:%s", host, port)
}

func canonicalEgressAuthority(authority, defaultPort string) (string, error) {
	host := authority
	port := defaultPort
	if strings.Contains(authority, ":") {
		var err error
		host, port, err = net.SplitHostPort(authority)
		if err != nil {
			return "", fmt.Errorf("invalid authority")
		}
	}
	domains, err := netpolicy.Normalize([]string{host})
	if err != nil {
		return "", fmt.Errorf("invalid domain")
	}
	if port != defaultPort {
		return "", fmt.Errorf("port not permitted")
	}
	return net.JoinHostPort(domains[0], port), nil
}
func stripProxyHeaders(h http.Header) {
	for _, v := range strings.Split(h.Get("Connection"), ",") {
		if v = strings.TrimSpace(v); v != "" {
			h.Del(v)
		}
	}
	for _, key := range []string{"Proxy-Authorization", "Proxy-Authenticate", "Proxy-Connection", "Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
}
func (p *egressProxy) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	r.Close = true
	rawHost := r.Host
	if c, ok := r.Context().Value(egressConnKey{}).(*trackedEgressConn); ok {
		rawHost = c.rawHost
	}
	deny := func(err error) { http.Error(w, err.Error(), http.StatusForbidden) }
	if r.Method == http.MethodConnect {
		authority, err := canonicalEgressAuthority(r.Host, "443")
		original, originalErr := canonicalEgressAuthority(rawHost, "443")
		if originalErr != nil || original != authority {
			deny(fmt.Errorf("egress denied: Host differs from CONNECT target"))
			return
		}
		if err != nil {
			deny(fmt.Errorf("egress denied: invalid HTTPS target"))
			return
		}
		target, err := p.transport.DialContext(r.Context(), "tcp", authority)
		if err != nil {
			deny(err)
			return
		}
		defer target.Close()
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		_ = client.SetDeadline(time.Now().Add(5 * time.Minute))
		_ = target.SetDeadline(time.Now().Add(5 * time.Minute))
		if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err = buffer.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(target, buffer); _ = target.Close(); close(done) }()
		_, _ = io.Copy(client, target)
		_ = client.Close()
		<-done
		return
	}
	if r.URL.Scheme != "http" || r.URL.User != nil {
		deny(fmt.Errorf("egress denied: only HTTP or HTTPS CONNECT supported"))
		return
	}
	authority, err := canonicalEgressAuthority(r.URL.Host, "80")
	if err != nil {
		deny(fmt.Errorf("egress denied: invalid HTTP target"))
		return
	}
	hostAuthority, err := canonicalEgressAuthority(rawHost, "80")
	if err != nil || hostAuthority != authority {
		deny(fmt.Errorf("egress denied: Host differs from target"))
		return
	}
	request := r.Clone(r.Context())
	request.RequestURI = ""
	request.URL.Host = authority
	request.Host = authority
	stripProxyHeaders(request.Header)
	response, err := p.transport.RoundTrip(request)
	if err != nil {
		var rejected *egressDenial
		if errors.As(err, &rejected) {
			deny(rejected)
		} else {
			deny(fmt.Errorf("egress request failed for %s", authority))
		}
		return
	}
	defer response.Body.Close()
	stripProxyHeaders(response.Header)
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

// bridge streams a single TCP connection to the host's Unix proxy socket.
func relayEgress(local, remote net.Conn) {
	defer local.Close()
	defer remote.Close()
	_ = local.SetDeadline(time.Now().Add(5 * time.Minute))
	_ = remote.SetDeadline(time.Now().Add(5 * time.Minute))
	done := make(chan struct{})
	go func() { _, _ = io.Copy(remote, bufio.NewReader(local)); _ = remote.Close(); close(done) }()
	_, _ = io.Copy(local, remote)
	_ = local.Close()
	<-done
}
