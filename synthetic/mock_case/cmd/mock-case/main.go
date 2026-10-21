// Command mock-case is the synthetic case system of the POC test environment (REQ-ENV-008, DEC-2026-10-16 E5).
// It is not a Noema deployable, holds no decision, grant, Record or provider credential, and exists only in test
// profiles. Every response and every clock it produces is SYNTHETIC.
//
//	mock-case                       serve the case API and the bench (truth and control) API
//	mock-case -healthcheck          exit 0 if the case listener answers /healthz (for container health checks)
//	mock-case init-credentials -file PATH ...
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

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/api"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/config"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/listen"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init-credentials" {
		os.Exit(initCredentials(os.Args[2:]))
	}
	healthcheck := flag.Bool("healthcheck", false, "probe /healthz on the case listener and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := serve(log); err != nil {
		log.Error("mock-case stopped", "err", err)
		os.Exit(1)
	}
}

func probe() int {
	_, port, err := net.SplitHostPort(config.OSEnv("MOCK_CASE_LISTEN"))
	if err != nil {
		port = "8090"
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

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
		if err := createIfMissing(path); err != nil {
			fmt.Fprintln(os.Stderr, "init-credentials:", err)
			return 1
		}
	}
	return 0
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// createIfMissing writes 48 random bytes as hex. The value is never printed. The file is world-readable inside
// its volume because the service, the database and the consumers run as different users; the volume is local.
func createIfMissing(path string) error {
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return nil
	}
	var raw [48]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(raw[:])+"\n"), 0o644)
}

func serve(log *slog.Logger) error {
	cfg, err := config.Load(config.OSEnv, config.OSReadFile)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := openWithRetry(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := api.New(cfg, st, log)
	caseLn, err := net.Listen("tcp", cfg.CaseListen)
	if err != nil {
		return err
	}
	var benchLn net.Listener
	if cfg.BenchBindVia != "" {
		benchLn, err = listen.Via(cfg.BenchBindVia, cfg.BenchPort)
	} else {
		benchLn, err = net.Listen("tcp", cfg.BenchListen)
	}
	if err != nil {
		return err
	}
	log.Info("mock-case listening (SYNTHETIC)", "case", caseLn.Addr().String(), "bench", benchLn.Addr().String())

	servers := []*http.Server{newHTTP(srv.CaseHandler()), newHTTP(srv.BenchHandler())}
	errc := make(chan error, 2)
	go func() { errc <- servers[0].Serve(caseLn) }()
	go func() { errc <- servers[1].Serve(benchLn) }()
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

// newHTTP sets read limits. There is deliberately no write timeout: a fault plan may hold a response for a
// long time (TIMEOUT_AFTER_COMMIT) and the caller decides when to give up.
func newHTTP(h http.Handler) *http.Server {
	return &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
}

// openWithRetry waits for the database: in Compose it starts at the same time as this service.
func openWithRetry(ctx context.Context, cfg *config.Config, log *slog.Logger) (*store.Store, error) {
	deadline := time.Now().Add(90 * time.Second)
	for {
		st, err := store.Open(ctx, cfg.AppDSN(), cfg.AdminDSN())
		if err == nil {
			if err = st.Migrate(ctx); err == nil {
				return st, nil
			}
			st.Close()
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, fmt.Errorf("database not ready: %w", err)
		}
		log.Info("waiting for the database", "err", err.Error())
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
