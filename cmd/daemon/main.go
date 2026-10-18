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

	"nekocode/runtime/httpapi"
	"nekocode/runtime/standard"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "HTTP listen address")
	token := flag.String("token", os.Getenv("NEKOCODE_DAEMON_TOKEN"), "optional bearer token for HTTP API")
	cors := flag.String("cors", os.Getenv("NEKOCODE_DAEMON_CORS"), "optional allowed CORS origins for browser dashboards (comma separated, * for any)")
	flag.Parse()

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

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.WithOptions(httpapi.New(rt).Handler(), httpapi.Options{Token: *token, AllowOrigin: *cors}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		printStartup(*addr, strings.TrimSpace(*token) != "", strings.TrimSpace(*cors) != "")
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
