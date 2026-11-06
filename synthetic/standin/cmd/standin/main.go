// Command standin serves the Noema-side stand-in for the Tushe-held US-01 and US-06 data (Option B for G1). SYNTHETIC. A test-environment
// component, not a Noema deployable: it is reached with benchmark credentials on the control side and the runtime credential on the data side.
//
//	standin               serve
//	standin -healthcheck  exit 0 if the data listener accepts a connection
//	standin init-credentials -file P ...  create each credential file with a random value if it is missing (the Compose init step)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Noemai-org/noema/synthetic/standin/internal/creds"
	"github.com/Noemai-org/noema/synthetic/standin/internal/listen"
	"github.com/Noemai-org/noema/synthetic/standin/internal/standin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init-credentials" {
		os.Exit(initCredentials(os.Args[2:]))
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := standin.LoadConfig(standin.OSEnv, standin.OSReadFile)
	if err != nil {
		log.Error("configuration refused", "error", err.Error())
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		conn, err := net.DialTimeout("tcp", cfg.DataListen, 2*time.Second)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = conn.Close() // a probe connection; a close error changes nothing
		return
	}
	if err := serve(log, cfg); err != nil {
		log.Error("stopped", "error", err.Error())
		os.Exit(1)
	}
}

// initCredentials takes -file=P (repeatable) and creates what is missing. Nothing is printed about the values.
func initCredentials(args []string) int {
	var paths []string
	for _, a := range args {
		p, ok := strings.CutPrefix(a, "-file=")
		if !ok || p == "" {
			fmt.Fprintln(os.Stderr, "init-credentials: arguments are -file=PATH, repeated")
			return 2
		}
		paths = append(paths, p)
	}
	if err := creds.Init(paths); err != nil {
		fmt.Fprintln(os.Stderr, "init-credentials:", err)
		return 1
	}
	return 0
}

func serve(log *slog.Logger, cfg *standin.Config) error {
	srv, err := standin.New(cfg.Options)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	dataLn, err := net.Listen("tcp", cfg.DataListen)
	if err != nil {
		return err
	}
	var controlLn net.Listener
	if cfg.ControlBindVia != "" {
		controlLn, err = listen.Via(cfg.ControlBindVia, cfg.ControlPort)
	} else {
		controlLn, err = net.Listen("tcp", cfg.ControlListen)
	}
	if err != nil {
		return err
	}
	log.Info("stand-in listening (SYNTHETIC)", "data", dataLn.Addr().String(), "control", controlLn.Addr().String())
	servers := []*http.Server{newHTTP(srv.DataHandler()), newHTTP(srv.ControlHandler())}
	errc := make(chan error, 2)
	go func() { errc <- servers[0].Serve(dataLn) }()
	go func() { errc <- servers[1].Serve(controlLn) }()
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdown) // the process is ending; a late connection is dropped
	}
	return nil
}

func newHTTP(h http.Handler) *http.Server {
	return &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
}
