package main

// STM-01 / noema#715, ruling A319 (committed in docs as an interim record): the bound of one investigation read is the setting NOEMA_READ_TIMEOUT_MS, in whole milliseconds, with
// NO default in code (AGENT-16; the value, 2000, is set by deployment). A missing or invalid value means the reader is not built and the error names the setting; a valid value is
// the bound the read runs under, and never later than the request's own deadline. REQ-API-001, REQ-STM-001; AGENT-16. DSNs and keys are SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// deadlinePool is the store's Pool seam: it remembers the deadline of the context the read begins its transaction with, and begins none. It stands for no failure mode: the read
// is only observed.
type deadlinePool struct {
	deadline time.Time
	has      bool
	calls    int
}

func (p *deadlinePool) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	p.calls++
	p.deadline, p.has = ctx.Deadline()
	return nil, errors.New("synthetic: this pool only records the deadline")
}

type oneStore struct{ s *investigations.Store }

func (o oneStore) For(string) (*investigations.Store, error) { return o.s, nil }

func observedReader(t *testing.T, ms string) (InvestigationReader, *deadlinePool) {
	t.Helper()
	pool := &deadlinePool{}
	st := &investigations.Store{Pool: pool, Appender: &chain.Appender{}, Inputs: investigations.ConfirmWith(nil)}
	r, err := buildInvestigationReader(Env{Getenv: envMap{EnvReadTimeoutMS: ms}.get}, oneStore{st})
	if err != nil || r == nil {
		t.Fatalf("a valid bound %q: %v, %v", ms, r, err)
	}
	return r, pool
}

// AGENT-16: no default. A missing value and every value that is not a whole positive number of milliseconds (a sign, a space, a leading zero, a unit, a fraction, an exponent, a
// non-ASCII digit, a value too large to be a duration) builds no reader and the error names the setting.
func TestTheReadBoundIsASettingWithNoDefaultAndParsedStrictly(t *testing.T) {
	for _, bad := range []string{"", "0", "-5", "+2000", " 2000", "2000 ", "2 000", "02000", "00", "2e3", "2000ms", "2s", "1.5", "0x10", "२०००", "9223372036854775807", "99999999999999999999"} {
		r, err := buildInvestigationReader(Env{Getenv: envMap{EnvReadTimeoutMS: bad}.get}, oneStore{})
		if err == nil || r != nil || !strings.Contains(err.Error(), EnvReadTimeoutMS) {
			t.Errorf("%q: reader %v, error %v; want no reader and an error that names %s", bad, r, err, EnvReadTimeoutMS)
		}
	}
	for _, good := range []string{"1", "2000", "60000"} {
		if r, err := buildInvestigationReader(Env{Getenv: envMap{EnvReadTimeoutMS: good}.get}, oneStore{}); err != nil || r == nil {
			t.Errorf("%q: %v, %v; want a reader", good, r, err)
		}
	}
}

// The value is the bound: a read whose request has no deadline runs under exactly that many milliseconds, a request with an earlier deadline keeps it, and a later one does not
// lengthen the bound.
func TestTheReadRunsUnderTheConfiguredBoundAndNeverALaterOneThanTheRequests(t *testing.T) {
	r, pool := observedReader(t, "700")
	before := time.Now()
	_, found, err := r.Read(context.Background(), "ten_aa", "prn_1", wireInvA1)
	if pool.calls != 1 || !pool.has || found || err == nil {
		t.Fatalf("calls %d, deadline %v, found %v, err %v; want one read under a deadline that failed", pool.calls, pool.has, found, err)
	}
	if got := pool.deadline.Sub(before); got <= 400*time.Millisecond || got > 700*time.Millisecond+time.Second/2 {
		t.Errorf("the read's deadline is %v after the call; want about 700ms", got)
	}
	early, cancelEarly := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelEarly()
	_, _, _ = r.Read(early, "ten_aa", "prn_1", wireInvA1)
	if got := pool.deadline.Sub(before); got > 300*time.Millisecond {
		t.Errorf("a request deadline of 100ms was lengthened to %v", got)
	}
	late, cancelLate := context.WithTimeout(context.Background(), time.Hour)
	defer cancelLate()
	_, _, _ = r.Read(late, "ten_aa", "prn_1", wireInvA1)
	if got := pool.deadline.Sub(before); got > 700*time.Millisecond+time.Second/2 {
		t.Errorf("a request deadline of one hour lengthened the bound to %v", got)
	}
	r2, pool2 := observedReader(t, "1500")
	before = time.Now()
	_, _, _ = r2.Read(context.Background(), "ten_aa", "prn_1", wireInvA1)
	if got := pool2.deadline.Sub(before); got <= 1000*time.Millisecond || got > 1500*time.Millisecond+time.Second/2 {
		t.Errorf("a bound of 1500ms ran under %v", got)
	}
}

// buildComponents: the setting is required exactly when the reader would be built (the orchestrator's settings are given). Missing or invalid is an error that names it, with no
// components and a closer that is safe to call; with a valid value the reader is built; with no orchestrator settings the setting is not asked for.
func TestTheReadBoundIsRequiredWhenTheOrchestratorSettingsAreGiven(t *testing.T) {
	rd, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	o := newOrchDirs(t, "ten_aa")
	base := func() envMap {
		return envMap{EnvRecordReadDSNDir: rd, EnvOrchestratorDSNDir: o.dsn, EnvOrchestratorKeyDir: o.keys, EnvOrchestratorKeyMaxActive: "2"}
	}
	for name, v := range map[string]*string{"missing": nil, "empty": ptr(""), "zero": ptr("0"), "a unit": ptr("2000ms"), "a leading zero": ptr("02000"), "a sign": ptr("+1")} {
		e := base()
		if v != nil {
			e[EnvReadTimeoutMS] = *v
		}
		c, closer, err := buildAll(t, e)
		if err == nil || !strings.Contains(err.Error(), EnvReadTimeoutMS) {
			t.Errorf("%s: error %v; want one naming %s", name, err, EnvReadTimeoutMS)
		}
		if c.Investigations != nil || c.Ports.Recorder != nil || c.Records != nil || c.Ports.Admissions != nil {
			t.Errorf("%s: components built with an error: %+v", name, c)
		}
		if closer == nil {
			t.Fatalf("%s: no closer", name)
		}
		closer()
	}
	e := base()
	e[EnvReadTimeoutMS] = "2000"
	c, closer, err := buildAll(t, e)
	if err != nil || c.Investigations == nil || c.Ports.Recorder == nil {
		t.Fatalf("a valid bound: %v, reader %v, recorder %v", err, c.Investigations != nil, c.Ports.Recorder != nil)
	}
	closer()
	// the setting is not asked for when there is no orchestrator: the Record reader alone builds, and a bound given without an orchestrator builds no reader and no error
	c, closer, err = buildAll(t, envMap{EnvRecordReadDSNDir: rd, EnvReadTimeoutMS: "x"})
	if err != nil || c.Investigations != nil {
		t.Errorf("no orchestrator: %v, reader %v; want no error and no reader", err, c.Investigations != nil)
	}
	closer()
}

func ptr(s string) *string { return &s }
