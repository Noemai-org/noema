// Command core is the Noema core service: Trust, Execution, Outcome, the Record, the Evidence
// Layer, the enterprise adapters and the Temporal workers (SoT §35.6).
//
// It serves the two health endpoints and, when the session settings are present, the protected /v1 API (EDG-01 slice 3c). It holds no product logic (PLT-01): the
// composition root is compose.go.
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

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
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

// newMux returns the health routes and, when api is not nil, the protected /v1 API. /healthz says the process is alive; /readyz says it can serve. The health routes
// are public and outside /v1; everything under /v1/ goes through api (the verified session first), never around it.
func newMux(check readiness, api http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	if api != nil {
		mux.Handle("/v1/", api)
	}
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
	defer func() { _ = resp.Body.Close() }() // the status is all that is read; a failed close of an unread body is not actionable
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The components other tasks build are plugged in here as their constructors land (nothing else is configured yet, so /readyz names the first one missing).
	env := Env{Getenv: os.Getenv, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	records, closeRecords, err := buildRecordReader(ctx, env)
	if err != nil {
		logger.Error("configuration", "error", err.Error()) // names the setting or the tenant, never a DSN
		os.Exit(1)
	}
	defer closeRecords()
	components := Components{Records: records, AccessLog: logger}
	clk := clock.System{}
	api, err := buildAPI(components, env, clk, ids.New(clk, nil))
	if err != nil {
		logger.Error("configuration", "error", err.Error()) // names the setting, never a value
		os.Exit(1)
	}
	srv := &http.Server{Addr: addr, Handler: newMux(composeReadiness(components, api), api), ReadHeaderTimeout: headerTimeout}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout) // ctx is already done: keep its values, not its cancellation
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
