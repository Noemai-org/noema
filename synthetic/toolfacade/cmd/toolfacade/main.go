// Command toolfacade serves the arm-facing tool facade (Architect ruling R1). A test-environment component, not a Noema deployable. SYNTHETIC.
//
//	toolfacade               serve the data side and the control side
//	toolfacade -healthcheck  exit 0 if the data listener accepts a connection (for a container health check)
package main

import (
	"context"
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

	"github.com/Noemai-org/noema/synthetic/toolfacade/internal/facade"
)

const (
	requestReadTimeout = 10 * time.Second
	shutdownTimeout    = 10 * time.Second
	healthDialTimeout  = 2 * time.Second
)

func main() {
	health := flag.Bool("healthcheck", false, "exit 0 if the data listener accepts a connection")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := facade.LoadConfig(facade.OSEnv, facade.OSReadFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "toolfacade:", err)
		os.Exit(2)
	}
	if *health {
		host, port, _ := net.SplitHostPort(cfg.DataListen)
		if host == "" {
			host = "127.0.0.1"
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), healthDialTimeout)
		if err != nil {
			os.Exit(1)
		}
		_ = conn.Close()
		return
	}
	s, err := facade.New(cfg.Options)
	if err != nil {
		fmt.Fprintln(os.Stderr, "toolfacade:", err)
		os.Exit(2)
	}
	data := facade.HTTPServer(cfg.DataListen, s.DataHandler(), requestReadTimeout)
	control := facade.HTTPServer(cfg.ControlListen, s.ControlHandler(), requestReadTimeout)
	errs := make(chan error, 2)
	for _, srv := range []*http.Server{data, control} {
		go func() { errs <- srv.ListenAndServe() }()
	}
	log.Info("toolfacade started", "data", cfg.DataListen, "control", cfg.ControlListen)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("listener stopped", "err", err.Error())
			os.Exit(1)
		}
	case <-stop:
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, srv := range []*http.Server{data, control} {
		_ = srv.Shutdown(ctx) // a shutdown error at exit changes nothing
	}
}
