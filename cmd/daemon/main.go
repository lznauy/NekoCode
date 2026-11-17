package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"nekocode/bot/modelcatalog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"nekocode/runtime/httpapi"
	"nekocode/runtime/standard"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "HTTP listen address")
	token := flag.String("token", os.Getenv("NEKOCODE_DAEMON_TOKEN"), "optional bearer token for HTTP API")
	cors := flag.String("cors", os.Getenv("NEKOCODE_DAEMON_CORS"), "optional allowed CORS origins for browser dashboards (comma separated, * for any)")
	enableA2A := flag.Bool("a2a", false, "enable the optional A2A Agent Card and protocol endpoints")
	a2aURL := flag.String("a2a-url", "", "public A2A base URL (requires -a2a; default: first local access URL plus /a2a)")
	flag.Parse()
	catalogCtx, cancelCatalog := context.WithCancel(context.Background())
	defer cancelCatalog()
	modelcatalog.RefreshInBackground(catalogCtx)

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

	handler, err := newDaemonHandler(rt, *addr, httpapi.Options{Token: *token, AllowOrigin: *cors}, *enableA2A, *a2aURL)
	if err != nil {
		_ = rt.Close()
		log.Fatalf("initialize HTTP server: %v", err)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		printStartup(*addr, strings.TrimSpace(*token) != "", strings.TrimSpace(*cors) != "")
		if *enableA2A {
			log.Print("A2A Agent Card: /.well-known/agent-card.json; A2A REST: /a2a")
		}
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
