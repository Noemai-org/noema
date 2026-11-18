// Package trustpack is the Trust intake's production adapters over the pack's data (TRU-02 ports; ruling A306, interim; A313, A319): Capabilities (T4, T11 to T14), Scope (T5)
// and Authority (T6), and the escalation set the Record's writer takes (REQ-TRU-010). A pack is data, loaded and never imported as code (SoT 51): no capability ID, node ID or
// industry term is written in this package (INV-14).
package trustpack

import (
	"errors"
	"io/fs"
)

// Errors of the adapters. Callers compare with errors.Is.
var (
	// ErrConfig is returned by an adapter that was not built by its constructor (the zero value), by a constructor given no pack or no port, and by a call on a nil pack: nothing is
	// ever answered from nothing.
	ErrConfig = errors.New("trustpack: the adapter is not configured")
	// ErrPack is returned by LoadPack for pack data that cannot give a sound answer. The error names the rule, never the content of the pack.
	ErrPack = errors.New("trustpack: the pack data is not usable")
	// ErrSession is returned for a question about no tenant.
	ErrSession = errors.New("trustpack: the question names no tenant")
	// ErrIntent is returned for an admitted intent that names no tenant, no intent ID or no version.
	ErrIntent = errors.New("trustpack: the admitted intent is not usable")
	// ErrCapability is returned by Authorized for a capability the registry does not know or that is prohibited: such a capability roots no authorization.
	ErrCapability = errors.New("trustpack: the capability cannot root an authorization")
	// ErrEscalationGraph is returned by EscalationSetFromPack for a graph that is missing from the pack or that has no WRITE node.
	ErrEscalationGraph = errors.New("trustpack: the escalation graph is missing or has no write node")
)

var errNotBuilt = errors.New("trustpack: not built")

// Pack is the pack's data as the adapters read it, loaded once and read-only after that.
type Pack struct{}

// LoadPack reads the pack file `packFile` from `root` and the action graph files it lists (paths relative to `root`).
func LoadPack(root fs.FS, packFile string) (*Pack, error) { return nil, errNotBuilt }
