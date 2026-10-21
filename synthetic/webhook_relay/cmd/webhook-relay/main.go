// Command webhook-relay is the webhook relay of DEC-2026-10-16 E6 (ENV-13): it receives Tushe's signed deliveries,
// verifies them, stores them, and lets the core pull them. A test-environment component, not a Noema deployable.
//
//	webhook-relay                           serve
//	webhook-relay -healthcheck              exit 0 if the local server answers /healthz
//	webhook-relay pull-token                print the pull token derived from the signing secret
//	webhook-relay init-credentials -file=P [-datadir=D]  write a fresh signing secret (mode 0444) if absent; create D for the relay
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/config"
	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/server"
	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/store"
	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/verify"
)

// relayUID is the distroless "nonroot" user the image runs as.
const relayUID = 65532

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-healthcheck":
			os.Exit(healthcheck())
		case "pull-token":
			cfg, err := config.OS()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println(verify.PullToken(cfg.Key))
			return
		case "init-credentials":
			if err := initCredentials(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
	cfg, err := config.OS()
	if err != nil {
		log.Error("configuration refused", "error", err.Error())
		os.Exit(1)
	}
	st, err := store.Open(cfg.Store)
	if err != nil {
		log.Error("store refused", "error", err.Error())
		os.Exit(1)
	}
	defer st.Close()
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.New(verify.New(cfg.Key), st, verify.PullToken(cfg.Key), time.Now).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("webhook relay listening (SYNTHETIC test environment)", "listen", cfg.Listen, "stored", st.Len())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func healthcheck() int {
	addr := os.Getenv("WEBHOOK_RELAY_LISTEN")
	if addr == "" {
		addr = ":8090"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 1
	}
	return 0
}

func initCredentials(args []string) error {
	fs := flag.NewFlagSet("init-credentials", flag.ContinueOnError)
	path := fs.String("file", "", "where to write the signing secret")
	dir := fs.String("datadir", "", "a directory to create for the store, owned by the relay's user")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("-file is required")
	}
	if *dir != "" {
		if err := os.MkdirAll(*dir, 0o700); err != nil {
			return err
		}
		if err := os.Chown(*dir, relayUID, relayUID); err != nil {
			return err
		}
	}
	if _, err := os.Stat(*path); err == nil {
		return nil // keep the secret across restarts
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	return os.WriteFile(*path, []byte("whsec_"+base64.StdEncoding.EncodeToString(b)), 0o444)
}
