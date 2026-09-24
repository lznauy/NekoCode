package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// OAuth discovery is supplied by the MCP server, not by the local user. Public
// servers must not turn discovery, registration or refresh into local requests.
// Only explicitly configured local/private MCP addresses opt into local access.
func oauthHTTPClient(mcpURL string) *http.Client {
	policy := newOAuthNetworkPolicy(mcpURL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy can resolve the destination independently, defeating IP validation.
	// OAuth uses direct connections; the MCP transport keeps its proxy behavior.
	transport.Proxy = nil
	transport.DialContext = policy.dial
	transport.Dial = nil
	transport.DialTLS = nil
	transport.DialTLSContext = nil
	return &http.Client{
		Timeout:       30 * time.Second,
		Transport:     endpointTransport{base: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type oauthNetworkPolicy struct {
	allowPrivate bool
	lookupIP     func(context.Context, string) ([]net.IPAddr, error)
	dialIP       func(context.Context, string, string) (net.Conn, error)
}

func newOAuthNetworkPolicy(mcpURL string) oauthNetworkPolicy {
	u, _ := url.Parse(mcpURL)
	allowPrivate := false
	if u != nil {
		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		ip, err := netip.ParseAddr(host)
		allowPrivate = host == "localhost" || (err == nil && (ip.Unmap().IsPrivate() || ip.Unmap().IsLoopback() || ip.Unmap().IsLinkLocalUnicast()))
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return oauthNetworkPolicy{allowPrivate: allowPrivate, lookupIP: net.DefaultResolver.LookupIPAddr, dialIP: dialer.DialContext}
}

func (p oauthNetworkPolicy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var ips []net.IPAddr
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IPAddr{{IP: ip}}
	} else {
		ips, err = p.lookupIP(ctx, host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("OAuth endpoint has no addresses")
	}
	// Validate every result before dialing any of them. Connect to a checked IP,
	// not the hostname, so a second lookup cannot rebind it into a private subnet.
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if !ok || (!p.allowPrivate && !oauthPublicIP(addr.Unmap())) {
			return nil, fmt.Errorf("public MCP OAuth endpoint cannot access a non-public address")
		}
	}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = p.dialIP(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, err
}

var oauthNonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// Translation/tunneling prefixes can conceal a private IPv4 destination.
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func oauthPublicIP(ip netip.Addr) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range oauthNonPublicPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
