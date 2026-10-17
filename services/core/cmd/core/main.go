// Command core is the Noema core service: Trust, Execution, Outcome, the Record, the Evidence
// Layer, the enterprise adapters and the Temporal workers (SoT §35.6).
//
// This skeleton serves only the two health endpoints. It holds no product logic (PLT-01).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	defaultAddr     = ":8080"
	shutdownTimeout = 10 * time.Second
	headerTimeout   = 5 * time.Second
	probeTimeout    = 2 * time.Second
)

// readiness reports whether every dependency the core needs is available. The skeleton has no
// dependencies wired yet, so it is never ready: /readyz answers 503 until a later task adds them.
type readiness func() (ready bool, reason string)

func notReady() (bool, string) {
	return false, "dependencies are not configured"
}

// newMux returns the health routes. /healthz says the process is alive; /readyz says it can serve.
func newMux(check readiness) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		ready, reason := check()
		w.Header().Set("Content-Type", "application/json")
		if ready {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ready"}` + "\n"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","reason":"` + reason + `"}` + "\n"))
	})
	return mux
}

// probeHealth asks a running core for /healthz. The image has no shell or curl, so the container
// health check runs the binary itself with -healthcheck.
func probeHealth(addr string) error {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Get("http://" + host + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe /healthz of a running core and exit")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := os.Getenv("NOEMA_CORE_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	if *healthcheck {
		if err := probeHealth(addr); err != nil {
			os.Exit(1)
		}
		return
	}
	srv := &http.Server{Addr: addr, Handler: newMux(notReady), ReadHeaderTimeout: headerTimeout}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown", "error", err.Error())
		}
	}()

	logger.Info("core listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("serve", "error", err.Error())
		os.Exit(1)
	}
}
