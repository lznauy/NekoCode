package main

import (
	"fmt"
	"net"
)

// printStartup prints the daemon's listen URLs and auth hints.
func printStartup(addr string, hasToken, hasCORS bool) {
	fmt.Println("NekoCode daemon listening")
	for _, u := range accessURLs(addr) {
		fmt.Println("  " + u)
	}
	if hasToken {
		fmt.Println("Auth: enabled")
		fmt.Println(`Example: curl -H "Authorization: Bearer $NEKOCODE_DAEMON_TOKEN" http://127.0.0.1:8765/runs`)
	} else {
		fmt.Println("Auth: disabled")
		if isWideBind(addr) {
			fmt.Println("Warning: this address is reachable from your network. Use -token or NEKOCODE_DAEMON_TOKEN for shared WiFi.")
		}
		fmt.Println(`Example: curl -X POST http://127.0.0.1:8765/input -d '{"text":"hello"}'`)
	}
	if hasCORS {
		fmt.Println("CORS: enabled for configured origins")
	}
	fmt.Println("Observe: /observe/overview, /observe/runs, /observe/metrics (Prometheus)")
	fmt.Println("SSE: /events")
}

func accessURLs(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{"http://" + addr}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if host != "0.0.0.0" && host != "::" {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	urls := []string{"http://127.0.0.1:" + port}
	for _, ip := range privateIPv4s() {
		urls = append(urls, "http://"+net.JoinHostPort(ip, port))
	}
	return urls
}

func privateIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ip := ipFromAddr(addr); ip != nil && ip.To4() != nil {
				out = append(out, ip.String())
			}
		}
	}
	return out
}

func ipFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func isWideBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "" || host == "0.0.0.0" || host == "::"
}
