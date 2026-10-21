// Command hostility-proxy is the fault-injecting proxy of the POC test environment (ENV-09; REQ-ENV-005). It is not a
// Noema deployable, holds no decision, grant, Record or provider credential, and runs only in the bench, ci and
// test profiles. It refuses to start in any other.
//
//	hostility-proxy                 serve the data plane and the control API
//	hostility-proxy -healthcheck    exit 0 if the data listener accepts connections (for container health checks)
//	hostility-proxy init-credentials -file PATH ...
//	                                create each file with a random value if it is missing (compose init step)
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/config"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/listen"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init-credentials" {
		os.Exit(initCredentials(os.Args[2:]))
	}
	healthcheck := flag.Bool("healthcheck", false, "probe the data listener and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := serve(log); err != nil {
		log.Error("hostility-proxy stopped", "err", err)
		os.Exit(1)
	}
}

func probe() int {
	_, port, err := net.SplitHostPort(config.OSEnv("HOSTILITY_PROXY_DATA_LISTEN"))
	if err != nil {
		port = "8080"
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
	if err != nil {
		return 1
	}
	conn.Close()
	return 0
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func initCredentials(args []string) int {
	fs := flag.NewFlagSet("init-credentials", flag.ExitOnError)
	var files multiFlag
	fs.Var(&files, "file", "a file to create if missing (repeatable)")
	fs.Parse(args) //nolint:errcheck
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "init-credentials: give at least one -file")
		return 2
	}
	for _, path := range files {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			continue
		}
		var raw [48]byte
		if _, err := rand.Read(raw[:]); err != nil {
			fmt.Fprintln(os.Stderr, "init-credentials:", err)
			return 1
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "init-credentials:", err)
			return 1
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(raw[:])+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "init-credentials:", err)
			return 1
		}
	}
	return 0
}

func serve(log *slog.Logger) error {
	cfg, err := config.Load(config.OSEnv, config.OSReadFile)
	if err != nil {
		return err
	}
	rawRoutes, err := os.ReadFile(cfg.RoutesFile)
	if err != nil {
		return err
	}
	table, err := routes.Parse(rawRoutes)
	if err != nil {
		return err
	}
	upstreams, err := cfg.Upstreams(table.UpstreamNames(), config.OSEnv)
	if err != nil {
		return err
	}
	fixtures, err := proxy.LoadFixtures(cfg.FixturesDir)
	if err != nil {
		return err
	}
	mapper, err := numbers.New(cfg.NumberKey)
	if err != nil {
		return err
	}
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: upstreams, Fixtures: fixtures, Numbers: mapper, BenchToken: cfg.BenchToken, Log: log})
	if err != nil {
		return err
	}
	dataLn, err := net.Listen("tcp", cfg.DataListen)
	if err != nil {
		return err
	}
	var ctlLn net.Listener
	if cfg.ControlBindVia != "" {
		ctlLn, err = listen.Via(cfg.ControlBindVia, cfg.ControlPort)
	} else {
		ctlLn, err = net.Listen("tcp", cfg.ControlListen)
	}
	if err != nil {
		return err
	}
	log.Info("hostility-proxy listening (SYNTHETIC)", "profile", cfg.Profile, "data", dataLn.Addr().String(), "control", ctlLn.Addr().String(),
		"upstreams", table.UpstreamNames(), "fixtures", len(fixtures))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	servers := []*http.Server{newHTTP(srv.DataHandler()), newHTTP(srv.ControlHandler())}
	errc := make(chan error, 2)
	go func() { errc <- servers[0].Serve(dataLn) }()
	go func() { errc <- servers[1].Serve(ctlLn) }()
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
		s.Shutdown(shutdown) //nolint:errcheck
	}
	return nil
}

// newHTTP sets read limits. There is no write timeout: a fault plan may hold a response for a long time and the
// caller decides when to give up.
func newHTTP(h http.Handler) *http.Server {
	return &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
}
