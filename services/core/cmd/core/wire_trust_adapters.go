package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// wire_trust_adapters.go builds the intake adapters that main code can build (STM-01 final composition; REQ-TRU-002, REQ-TRU-004, REQ-TRU-005; INV-03, INV-16, AGENT-16). Today that is
// the three that read the pack: Capabilities, Scope and Authority (trustpack). Every other adapter waits on a data source or a pack load that is not on main (see
// status/STM-01-main-composition.md), and is named in one startup error: no fake, no invented source and no quiet nil. A pack-free adapter does not exist: each reads the pack.

// EnvTrustEscalationGraph names the escalation graph of the pack (A319, A346: the pack has no field for it). Required once a pack is given; no default (A330).
const EnvTrustEscalationGraph = "NOEMA_TRUST_ESCALATION_GRAPH"

// PackSource gives the manifest-verified pack (A333, noema#773). Production has none until the pack load is on main: it must never read an unverified pack.
type PackSource func(env Env) (*trustpack.Pack, error)

var errPackNotOnMain = errors.New("cmd/core: the manifest-verified pack is not available: its load is not on main")

// productionPack is the production PackSource: none. Replacing it is the one change noema#773 makes here.
func productionPack(Env) (*trustpack.Pack, error) { return nil, errPackNotOnMain }

// UnbuiltAdaptersError names every Trust adapter that could not be built, in the order of the TrustAdapters struct ("Pack" first when there is no pack). Cause is the pack's own error, if any;
// the text carries names only, never a path, a setting's value or a DSN.
type UnbuiltAdaptersError struct {
	Names []string
	Cause error
}

func (e *UnbuiltAdaptersError) Error() string {
	return "cmd/core: the Trust service cannot start: not built: " + strings.Join(e.Names, ", ")
}

// Unwrap gives the cause.
func (e *UnbuiltAdaptersError) Unwrap() error { return e.Cause }

// unsetAdapters returns the name of every field of a that is not set, in the order of the struct: a nil interface, pointer, func, map, slice or channel (a typed nil inside an interface
// too), or an empty string. A typed nil is missing (INV-03, AGENT-16: no quiet nil).
func unsetAdapters(a TrustAdapters) []string {
	var names []string
	v := reflect.ValueOf(a)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		missing := false
		switch f.Kind() {
		case reflect.Interface, reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
			missing = f.IsNil() || (f.Kind() == reflect.Interface && f.Elem().Kind() == reflect.Pointer && f.Elem().IsNil())
		case reflect.String:
			missing = f.String() == ""
		}
		if missing {
			names = append(names, v.Type().Field(i).Name)
		}
	}
	return names
}

// trustAdaptersSource is the production TrustAdapters source over a pack source. It returns the adapters it built together with an *UnbuiltAdaptersError naming the rest, so a caller
// that wants to start Trust stops. With no pack it builds nothing and names "Pack" first. A setting error (the escalation graph) is its own error and builds nothing.
func trustAdaptersSource(pack PackSource) func(Env, reconstruct.TenantDBs, clock.Clock) (TrustAdapters, error) {
	return func(env Env, _ reconstruct.TenantDBs, clk clock.Clock) (TrustAdapters, error) {
		p, err := pack(env)
		if err != nil || p == nil {
			if err == nil {
				err = errPackNotOnMain
			}
			return TrustAdapters{}, &UnbuiltAdaptersError{Names: append([]string{"Pack"}, unsetAdapters(TrustAdapters{Clock: clk})...), Cause: err}
		}
		graph := env.Getenv(EnvTrustEscalationGraph)
		if graph == "" {
			return TrustAdapters{}, fmt.Errorf("cmd/core: the Trust adapters need %s", EnvTrustEscalationGraph)
		}
		caps, err := trustpack.NewCapabilities(p, graph)
		if err != nil {
			return TrustAdapters{}, fmt.Errorf("cmd/core: %s: %w", EnvTrustEscalationGraph, err)
		}
		scope, err := trustpack.NewScope(p)
		if err != nil {
			return TrustAdapters{}, fmt.Errorf("cmd/core: the pack's scope: %w", err)
		}
		authority, err := trustpack.NewAuthority(p)
		if err != nil {
			return TrustAdapters{}, fmt.Errorf("cmd/core: the pack's authority: %w", err)
		}
		a := TrustAdapters{Capabilities: caps, Scope: scope, Authority: authority, Clock: clk}
		return a, &UnbuiltAdaptersError{Names: unsetAdapters(a)}
	}
}
