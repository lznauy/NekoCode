package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"nekocode/runtime/a2aapi"
	"nekocode/runtime/httpapi"
	"nekocode/runtime/standard"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "HTTP listen address")
	token := flag.String("token", os.Getenv("NEKOCODE_DAEMON_TOKEN"), "optional bearer token for HTTP API")
	a2aURL := flag.String("a2a-url", "", "public base URL advertised by A2A (default: first local access URL plus /a2a)")
	flag.Parse()
	if err := validateListenSecurity(*addr, *token); err != nil {
		log.Fatal(err)
	}

	rt, err := standard.New()
	if err != nil {
		log.Fatalf("initialize runtime: %v", err)
	}
	statuses, err := bootstrapConnectors(context.Background(), rt.Connect, os.Getenv)
	if err != nil {
		_ = rt.Close()
		log.Fatalf("initialize connectors: %v", err)
	}
	for _, status := range statuses {
		log.Printf("connector %s: %s", status.name, status.message)
	}

	publicA2AURL := strings.TrimRight(strings.TrimSpace(*a2aURL), "/")
	if publicA2AURL == "" {
		publicA2AURL = strings.TrimRight(accessURLs(*addr)[0], "/") + "/a2a"
	}
	a2aServer, err := a2aapi.New(rt, a2aapi.Options{
		Endpoint: publicA2AURL,
		Secured:  strings.TrimSpace(*token) != "",
	})
	if err != nil {
		_ = rt.Close()
		log.Fatalf("initialize A2A server: %v", err)
	}

	protectedAPI := httpapi.WithBearerAuth(httpapi.New(rt).Handler(), *token)
	protectedA2A := httpapi.WithBearerAuth(http.StripPrefix("/a2a", a2aServer.Handler()), *token)
	mux := http.NewServeMux()
	mux.Handle("GET /.well-known/agent-card.json", a2aServer.AgentCardHandler())
	mux.Handle("/a2a/", protectedA2A)
	mux.Handle("/", protectedAPI)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		printStartup(*addr, strings.TrimSpace(*token) != "")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("daemon failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("daemon shutdown error: %v", err)
	}
	if err := rt.Close(); err != nil {
		log.Printf("runtime shutdown error: %v", err)
	}
}
