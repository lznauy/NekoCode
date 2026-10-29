package impl

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublicEgressAddressRejectsSiteLocalIPv6(t *testing.T) {
	for _, tc := range []struct {
		address string
		public  bool
	}{
		{address: "febf::1"},
		{address: "fec0::1"},
		{address: "fed0::1"},
		{address: "feff::1"},
		{address: "fe80::1"},
		{address: "2606:4700::1111", public: true},
	} {
		t.Run(tc.address, func(t *testing.T) {
			if got := publicEgressAddress(netip.MustParseAddr(tc.address)); got != tc.public {
				t.Fatalf("publicEgressAddress(%s) = %t, want %t", tc.address, got, tc.public)
			}
		})
	}
}

func TestDialEgressRejectsSiteLocalDNSFailClosed(t *testing.T) {
	dialed := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("2606:4700::1111"),
			netip.MustParseAddr("fec0::1"),
		}, nil
	}
	if conn, err := dialEgress(t.Context(), "example.com:443", []string{"example.com"}, lookup, dial); err == nil {
		conn.Close()
		t.Fatal("site-local DNS result accepted")
	}
	if dialed {
		t.Fatal("denied request dialed")
	}
}

func TestEgressPinsDNSAndRejectsPrivateTargets(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "::ffff:192.168.0.1", "100.64.0.1", "64:ff9b::a00:1", "0.0.0.0", "ff02::1"} {
		if publicEgressAddress(netip.MustParseAddr(address)) {
			t.Errorf("public: %s", address)
		}
	}
	calls := 0
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		calls++
		if network != "tcp" || address != "93.184.216.34:443" {
			t.Errorf("unvalidated destination: %s %s", network, address)
		}
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	conn, err := dialEgress(t.Context(), "example.com:443", []string{"example.com"}, lookup, dial)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if calls != 1 {
		t.Fatal(calls)
	}
	for _, target := range []string{"evil.com:443", "example.com:22", "127.0.0.1:443"} {
		if conn, err := dialEgress(t.Context(), target, []string{"example.com"}, lookup, dial); err == nil {
			conn.Close()
			t.Errorf("accepted %s", target)
		}
	}
	lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")}, nil
	}
	if _, err := dialEgress(t.Context(), "example.com:443", []string{"example.com"}, lookup, dial); err == nil {
		t.Fatal("mixed public/private resolution accepted")
	}
	if calls != 1 {
		t.Fatal("denied request dialed")
	}
}
func TestEgressHTTPPolicyAndCleanup(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credential leaked")
		}
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "http://evil.example/")
			w.WriteHeader(302)
			return
		}
		io.WriteString(w, "allowed")
	}))
	defer upstream.Close()
	p, err := startEgressProxy([]string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialEgress(ctx, address, []string{"example.com"}, func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}, func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
		})
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", p.socket())
	}}
	defer transport.CloseIdleConnections()
	// Send proxy-format requests over the Unix socket to exercise exact Host
	// handling without letting an HTTP client rewrite the authority.
	request := func(target, host string) string {
		t.Helper()
		c, err := net.Dial("unix", p.socket())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		io.WriteString(c, "GET "+target+" HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\nProxy-Authorization: secret\r\n\r\n")
		data, err := io.ReadAll(c)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := request("http://example.com/", "example.com"); !strings.Contains(got, "allowed") {
		t.Fatal(got)
	}
	if got := request("http://example.com/redirect", "example.com"); !strings.Contains(got, "302") {
		t.Fatal(got)
	}
	for _, tc := range [][2]string{{"http://evil.example/", "evil.example"}, {"http://example.com/", "other.example"}, {"http://example.com:22/", "example.com:22"}} {
		if got := request(tc[0], tc[1]); !strings.Contains(got, "403") {
			t.Fatal(got)
		}
	}
	socket := p.socket()
	p.Close()
	p.Close()
	if c, err := net.Dial("unix", socket); err == nil {
		c.Close()
		t.Fatal("proxy still running")
	}
}

func TestEgressHTTPSConnectAndPolicyIsolation(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "TLS-allowed") }))
	defer tlsServer.Close()
	makeProxy := func(domain string) *egressProxy {
		t.Helper()
		domains := []string{domain}
		p, err := startEgressProxy(domains)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		p.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialEgress(ctx, address, domains, func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}, func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, tlsServer.Listener.Addr().String())
			})
		}
		return p
	}
	proxies := []*egressProxy{makeProxy("example.com"), makeProxy("other.com")}
	var wg sync.WaitGroup
	for index, p := range proxies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			proxyURL, _ := url.Parse("http://proxy.invalid")
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", p.socket())
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			response, err := client.Get("https://example.com/")
			if index == 1 {
				if err == nil {
					response.Body.Close()
					t.Error("other policy allowed example.com")
				}
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			defer response.Body.Close()
			data, err := io.ReadAll(response.Body)
			if err != nil || string(data) != "TLS-allowed" {
				t.Errorf("TLS tunnel: %s %v", data, err)
			}
		}()
	}
	wg.Wait()
}
