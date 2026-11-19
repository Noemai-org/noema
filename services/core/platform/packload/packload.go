// Package packload loads a pack through its manifest and verifies it (UND-04, wiring slice 1; rulings A320 and A333; REQ-OPS-003, REQ-CTX-007; INV-14, INV-21; AGENT-13, AGENT-16).
//
// The manifest lists the pack files the core loads, each with its SHA-256, and names the ids the core expects of the evidence mapping and the reconciliation rule set, so the core
// hard-codes none of them (INV-14). Load reads the manifest and every listed file from one mounted directory and refuses the whole pack when anything differs from what the manifest
// pins: a file changed after CI, a path that escapes the directory, a link in any component, a file of a listed kind that is not listed, an id that is not the manifest's.
// Nothing is returned for a refused pack, so a half-verified pack never reaches a parser.
//
// May import: the standard library only. Packs are data (SoT 51): this package never imports a pack and holds no name of a vertical, a folder or a file of one.
//
// Must never: follow a link, read outside the directory, read a file it did not hash, accept a digest that is not 64 lower-case hex characters, default a bound, or put a path, a file's
// content or an operating-system error into an error text (AGENT-13; the cause stays reachable with errors.Is and errors.As).
package packload

import "errors"

// Pack is a verified pack: the bytes each digest covers and the ids the manifest names (`<id>@<version>`, as the files carry them).
type Pack struct {
	Pack              []byte
	EvidenceMapping   []byte
	ReconRules        []byte
	EvidenceMappingID string
	ReconRuleSetID    string
}

// The kinds of refusal. Callers compare with errors.Is.
var (
	ErrConfig   = errors.New("packload: the directory, the manifest path or the size bound is not usable")
	ErrManifest = errors.New("packload: the manifest is not usable")
	ErrPath     = errors.New("packload: a path is not a plain path inside the pack directory")
	ErrLink     = errors.New("packload: a path has a link in it")
	ErrFile     = errors.New("packload: a file is missing, is not a plain file or is over the size bound")
	ErrHash     = errors.New("packload: a file does not match its pinned SHA-256")
	ErrUnlisted = errors.New("packload: a file of a listed kind is not listed")
	ErrID       = errors.New("packload: a file carries another id than the manifest expects")
)

// Error is a refusal; the stub of the red commit refuses everything.
type Error struct {
	Err  error
	Kind string
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Load verifies and reads the pack that the manifest at manifest (relative to dir) lists. maxBytes bounds the manifest and each listed file.
func Load(dir, manifest string, maxBytes int64) (Pack, error) {
	return Pack{}, errors.New("packload: not implemented")
}
