// Package packload loads a pack through its manifest and verifies it (UND-04, wiring slice 1; rulings A320 and A333; REQ-OPS-003, REQ-CTX-007; INV-14, INV-21; AGENT-13, AGENT-16).
//
// The manifest lists the pack files the core loads, each with its SHA-256, and names the ids the core expects of the evidence mapping and the reconciliation rule set, so the core
// hard-codes none of them (INV-14). Load reads the manifest and every listed file from one mounted directory and refuses the whole pack when anything differs from what the manifest
// pins: a file changed after CI, a path that escapes the directory, a link in any component, a file of a listed kind that is not listed, an id that is not the manifest's, an action graph the pack names and the manifest does not pin (or the other way round: A510, no unverified graph is ever served).
// Nothing is returned for a refused pack, so a half-verified pack never reaches a parser.
//
// May import: the standard library only. Packs are data (SoT 51): this package never imports a pack and holds no name of a vertical, a folder or a file of one.
//
// Must never: follow a link, read outside the directory, read a file it did not hash, accept a digest that is not 64 lower-case hex characters, default a bound, or put a path, a file's
// content or an operating-system error into an error text (AGENT-13; the cause stays reachable with errors.Is and errors.As).
package packload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Pack is a verified pack: the bytes each digest covers and the ids the manifest names (`<id>@<version>`, as the files carry them).
type Pack struct {
	Pack              []byte
	EvidenceMapping   []byte
	ReconRules        []byte
	EvidenceMappingID string
	ReconRuleSetID    string
	// ActionGraphs holds the verified bytes of each action graph the pack names (A510), by the path the manifest pins. A graph that is not pinned is never here.
	ActionGraphs map[string][]byte
	// InjectionPatterns holds the verified bytes of the injection-screen pattern file the manifest pins (A645), and InjectionPatternsID the id the manifest expects of it. A file that is not pinned
	// and verified is never here. A manifest below version 4 may list none (A664, interim): then both are empty and the pack carries no pattern data, so no screen can be built from it (the injection
	// package has no empty Detector). A551: read-only, derived from the verified bytes, no industry term.
	InjectionPatterns   []byte
	InjectionPatternsID string
	// Limits holds the verified bytes of each limits file the manifest pins (A544), by the path the manifest pins. Which file is which, and what it holds, is the caller's: this package
	// names none of them. A limits file that is not pinned is never here, and a folder of limits files with one unpinned refuses the pack (ErrUnlisted).
	Limits map[string][]byte
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
	ErrGraph    = errors.New("packload: the action graphs the pack names are not the ones the manifest pins")
)

// Error is a refusal. Err is one of the kinds above; Kind names what was refused from a closed set (`manifest`, `pack`, `evidence_mapping`, `recon_rules`, `action_graph`, `limits` or `injection_patterns`), never a path, a digest or content.
type Error struct {
	Err  error
	Kind string
}

func (e *Error) Error() string {
	if e.Kind == "" {
		return e.Err.Error()
	}
	return e.Err.Error() + " (" + e.Kind + ")"
}

func (e *Error) Unwrap() error { return e.Err }

func refuse(err error, kind string) error { return &Error{Err: err, Kind: kind} }

// The kinds of file a manifest lists, as manifest.v1 spells them, and the one name of the manifest itself in a refusal.
const (
	kindPack     = "pack"
	kindMapping  = "evidence_mapping"
	kindRules    = "recon_rules"
	kindGraph    = "action_graph"
	kindLimits   = "limits"
	kindPatterns = "injection_patterns"

	// limitsFromVersion is the first manifest version that must list the limits files (A579, interim).
	limitsFromVersion = 3
	// patternsFromVersion is the first manifest version that must list the injection patterns (A664, interim; the A579 pattern, the version after the limits files' 3).
	patternsFromVersion = 4
	kindManifest        = "manifest"
)

var (
	// pathRe is manifest.v1's path: plain components of letters, digits, underscore, dot and hyphen, the first character of each a letter, digit or underscore, so there is no `..`, no
	// leading `/`, no `\`, no drive letter, no hidden component and no control character.
	pathRe = regexp.MustCompile(`\A[A-Za-z0-9_][A-Za-z0-9_.-]*(/[A-Za-z0-9_][A-Za-z0-9_.-]*)*\z`)
	// digestRe is a SHA-256 in lower-case hex.
	digestRe = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
	// idRe is `<id>@<version>`, as manifest.v1 spells the ids the core expects; manifestIDRe is the id of the manifest itself.
	idRe         = regexp.MustCompile(`\A[a-z][a-z0-9_]*(\.[a-z0-9_]+)*@[1-9][0-9]{0,8}\z`)
	manifestIDRe = regexp.MustCompile(`\A[a-z][a-z0-9_]*(\.[a-z0-9_]+)*\z`)
	// versionRe is a file's own version as an integer literal: a string, a fraction and a boolean are not one.
	versionRe = regexp.MustCompile(`\A[1-9][0-9]{0,8}\z`)
	// familyRe splits the name of a listed file that sits in the pack directory itself into the part that names its family and a version: `x_v1.json` is of the family `x_v`.
	familyRe = regexp.MustCompile(`\A(.*[._-]v)[0-9]+(\.[0-9]+)*\.[A-Za-z0-9]+\z`)
)

// The longest path, expected id and note manifest.v1 allows.
const (
	maxPathLen = 256
	maxIDLen   = 128
	maxNoteLen = 4000
	label      = "SYNTHETIC" // every pack of the POC is synthetic (AGENT-25); a manifest that says otherwise is not one this build was written for
)

type manifestFile struct {
	Kind   *string `json:"kind"`
	Path   *string `json:"path"`
	Sha256 *string `json:"sha256"`
}

type manifestDoc struct {
	ManifestID *string `json:"manifest_id"`
	Version    *int    `json:"version"`
	Label      *string `json:"label"`
	Note       *string `json:"note"`
	Expected   *struct {
		MappingID  *string `json:"evidence_mapping_id"`
		RulesID    *string `json:"recon_rule_set_id"`
		PatternsID *string `json:"injection_patterns_id"`
	} `json:"expected"`
	Files []manifestFile `json:"files"`
}

// Load verifies and reads the pack that the manifest at manifest (relative to dir) lists. dir is the mount the caller was given and is not itself examined for a link; every component
// below it is. maxBytes bounds the manifest and each listed file and has no default (AGENT-16).
func Load(dir, manifest string, maxBytes int64) (Pack, error) {
	if dir == "" || maxBytes < 1 {
		return Pack{}, refuse(ErrConfig, "")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Pack{}, refuse(ErrConfig, "")
	}
	defer func() { _ = root.Close() }() // read-only handle: nothing is flushed, so a close error cannot change what was read
	raw, err := readPlain(root, manifest, maxBytes, ErrManifest, kindManifest)
	if err != nil {
		return Pack{}, err
	}
	doc, err := decodeManifest(raw)
	if err != nil {
		return Pack{}, err
	}
	listed := map[string]bool{manifest: true}
	bytesOf := map[string][]byte{}
	graphs := map[string][]byte{}
	limits := map[string][]byte{}
	for _, f := range doc.Files {
		listed[*f.Path] = true
	}
	for _, f := range doc.Files {
		data, err := readPlain(root, *f.Path, maxBytes, ErrFile, *f.Kind)
		if err != nil {
			return Pack{}, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != *f.Sha256 {
			return Pack{}, refuse(ErrHash, *f.Kind)
		}
		switch *f.Kind {
		case kindGraph:
			graphs[*f.Path] = data
		case kindLimits:
			limits[*f.Path] = data
		default:
			bytesOf[*f.Kind] = data
		}
	}
	for _, f := range doc.Files {
		if err := noUnlisted(root, *f.Path, *f.Kind, listed); err != nil {
			return Pack{}, err
		}
	}
	p := Pack{Pack: bytesOf[kindPack], EvidenceMapping: bytesOf[kindMapping], ReconRules: bytesOf[kindRules],
		EvidenceMappingID: *doc.Expected.MappingID, ReconRuleSetID: *doc.Expected.RulesID}
	if err := checkIDs(p); err != nil {
		return Pack{}, err
	}
	if err := checkGraphs(p.Pack, graphs); err != nil {
		return Pack{}, err
	}
	// A664 (interim): a manifest below patternsFromVersion that lists no patterns serves none (nil bytes, empty id); a listed file is checked against its expected id like every other.
	patternsID := ""
	if doc.Expected.PatternsID != nil {
		patternsID = *doc.Expected.PatternsID
		id, ver, _, err := members(bytesOf[kindPatterns], "pack_id", "")
		if err != nil || id+"@"+ver != patternsID {
			return Pack{}, refuse(ErrID, kindPatterns)
		}
	}
	p.InjectionPatterns, p.InjectionPatternsID = bytesOf[kindPatterns], patternsID
	p.ActionGraphs = graphs
	p.Limits = limits
	return p, nil
}

// decodeManifest reads the manifest exactly as manifest.v1 has it: every required member, none unknown, none repeated, the label SYNTHETIC, a positive version, the expected ids in
// their shape, exactly one file of each of the three single kinds, any number of action graphs (the pack file's own list decides which, in Load), each with its digest and a path that is not listed twice.
func decodeManifest(raw []byte) (manifestDoc, error) {
	bad := refuse(ErrManifest, kindManifest)
	if repeatsAMember(raw) {
		return manifestDoc{}, bad
	}
	var doc manifestDoc
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return manifestDoc{}, bad
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return manifestDoc{}, bad
	}
	if doc.ManifestID == nil || !manifestIDRe.MatchString(*doc.ManifestID) || doc.Version == nil || *doc.Version < 1 || doc.Label == nil || *doc.Label != label ||
		doc.Expected == nil || doc.Expected.MappingID == nil || doc.Expected.RulesID == nil || len(doc.Files) == 0 {
		return manifestDoc{}, bad
	}
	if doc.Note != nil && (*doc.Note == "" || len(*doc.Note) > maxNoteLen) {
		return manifestDoc{}, bad
	}
	for _, id := range []string{*doc.Expected.MappingID, *doc.Expected.RulesID} {
		if !idRe.MatchString(id) || len(id) > maxIDLen {
			return manifestDoc{}, bad
		}
	}
	perKind, paths := map[string]int{}, map[string]bool{}
	for _, f := range doc.Files {
		if f.Kind == nil || f.Path == nil || f.Sha256 == nil || !digestRe.MatchString(*f.Sha256) {
			return manifestDoc{}, bad
		}
		if k := *f.Kind; k != kindPack && k != kindMapping && k != kindRules && k != kindGraph && k != kindLimits && k != kindPatterns {
			return manifestDoc{}, bad
		}
		perKind[*f.Kind]++
		if paths[*f.Path] {
			return manifestDoc{}, bad
		}
		paths[*f.Path] = true
	}
	if perKind[kindPack] != 1 || perKind[kindMapping] != 1 || perKind[kindRules] != 1 {
		return manifestDoc{}, bad
	}
	// A579 (interim; SOT-C-33): a manifest of version 3 or later lists its limits files, and a pack whose manifest does not is refused. There is no opt-in.
	if *doc.Version >= limitsFromVersion && perKind[kindLimits] == 0 {
		return manifestDoc{}, bad
	}
	// A645, A664 (interim; the A579 pattern): the injection patterns are listed exactly once from manifest version patternsFromVersion and none or once below it, always together with their expected id
	// and never one without the other. There is no opt-in for a gated version.
	if n := perKind[kindPatterns]; n > 1 || (*doc.Version >= patternsFromVersion && n != 1) || (n == 1) != (doc.Expected.PatternsID != nil) {
		return manifestDoc{}, bad
	}
	if id := doc.Expected.PatternsID; id != nil && (!idRe.MatchString(*id) || len(*id) > maxIDLen) {
		return manifestDoc{}, bad
	}
	return doc, nil
}

// repeatsAMember reports whether any object in the document names a member twice, at any depth (encoding/json keeps the last one and says nothing), or the document is not one value.
func repeatsAMember(raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	return walk(dec) != nil
}

func walk(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for dec.More() {
		if d == '{' {
			k, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := k.(string)
			if seen[key] {
				return errors.New("repeated")
			}
			seen[key] = true
		}
		if err := walk(dec); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

// readPlain reads the plain file at rel, below the root, whole. A component that is a link is ErrLink; a path off its shape is ErrPath; a file that is missing, not a regular file or
// over the bound is notUsable (ErrFile for a pack file, ErrManifest for the manifest). It reads through the root, so even a component that is swapped for a link after the check cannot
// lead outside the directory.
func readPlain(root *os.Root, rel string, maxBytes int64, notUsable error, kind string) ([]byte, error) {
	if !pathRe.MatchString(rel) || len(rel) > maxPathLen {
		return nil, refuse(ErrPath, kind)
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		fi, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		switch {
		case err != nil:
			return nil, refuse(notUsable, kind)
		case fi.Mode()&fs.ModeSymlink != 0:
			return nil, refuse(ErrLink, kind)
		}
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, refuse(notUsable, kind)
	}
	defer func() { _ = f.Close() }() // read-only handle, as above
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, refuse(notUsable, kind)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, refuse(notUsable, kind)
	}
	return data, nil
}

// noUnlisted refuses a file of the listed file's kind that the manifest does not list, a second candidate for the same role. Where a kind lives is not the core's to know, so it is read
// from where the manifest put the listed file: a file that sits in the pack directory itself has a family (`x_v1.json` and `x_v2.json`), and any other file has its folder, in which
// every file of the same extension, at any depth, is of its kind. A link counts, dangling or not, and is not followed. The manifest itself is not a file of any kind.
func noUnlisted(root *os.Root, rel, kind string, listed map[string]bool) error {
	dir, name := path.Split(rel)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		m := familyRe.FindStringSubmatch(name)
		if m == nil {
			return nil
		}
		return scan(root, ".", "", kind, listed, func(entry string) bool {
			f := familyRe.FindStringSubmatch(entry)
			return f != nil && f[1] == m[1]
		}, false)
	}
	ext := path.Ext(name)
	return scan(root, dir, dir+"/", kind, listed, func(entry string) bool { return path.Ext(entry) == ext }, true)
}

func scan(root *os.Root, dir, prefix, kind string, listed map[string]bool, of func(name string) bool, deep bool) error {
	d, err := root.Open(dir)
	if err != nil {
		return refuse(ErrFile, kind)
	}
	entries, err := d.ReadDir(-1)
	_ = d.Close() // read-only directory handle; the ReadDir error below is the one that counts
	if err != nil {
		return refuse(ErrFile, kind)
	}
	for _, e := range entries {
		rel := prefix + e.Name()
		if e.IsDir() && deep {
			if err := scan(root, rel, rel+"/", kind, listed, of, deep); err != nil {
				return err
			}
			continue
		}
		if !e.IsDir() && of(e.Name()) && !listed[rel] {
			return refuse(ErrUnlisted, kind)
		}
	}
	return nil
}

// checkIDs reads the id each of the evidence mapping and the rule set carries and requires it to be the one the manifest expects; the mapping must also name the expected rule set as
// the one it was written for.
func checkIDs(p Pack) error {
	mid, mver, rs, err := members(p.EvidenceMapping, "mapping_id", "ruleset")
	if err != nil || mid+"@"+mver != p.EvidenceMappingID || rs != p.ReconRuleSetID {
		return refuse(ErrID, kindMapping)
	}
	rid, rver, _, err := members(p.ReconRules, "ruleset_id", "")
	if err != nil || rid+"@"+rver != p.ReconRuleSetID {
		return refuse(ErrID, kindRules)
	}
	return nil
}

// members reads a file's id member, its integer version and, when asked, one more string member.
func members(data []byte, idKey, extraKey string) (id, version, extra string, err error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return "", "", "", err
	}
	if err := json.Unmarshal(m[idKey], &id); err != nil || id == "" {
		return "", "", "", errors.New("id")
	}
	v := string(m["version"])
	if !versionRe.MatchString(v) {
		return "", "", "", errors.New("version")
	}
	if _, err := strconv.Atoi(v); err != nil {
		return "", "", "", err
	}
	if extraKey != "" {
		if err := json.Unmarshal(m[extraKey], &extra); err != nil {
			return "", "", "", err
		}
	}
	return id, v, extra, nil
}

// checkGraphs requires the action graphs the manifest pins to be exactly the ones the verified pack file names in `action_graph_files` (A510): a graph the pack names and the manifest does not
// pin would be served unverified, and a graph pinned for a pack that does not name it is a file nobody asked for. A pack that names none, or has no such member, pins none. The pack file is read
// only here, from the bytes its digest covers.
func checkGraphs(packBytes []byte, pinned map[string][]byte) error {
	bad := refuse(ErrGraph, kindGraph)
	var doc struct {
		Named json.RawMessage `json:"action_graph_files"`
	}
	if err := json.Unmarshal(packBytes, &doc); err != nil {
		return bad
	}
	var named []string
	if len(doc.Named) > 0 && string(doc.Named) != "null" {
		if err := json.Unmarshal(doc.Named, &named); err != nil {
			return bad
		}
	}
	seen := make(map[string]bool, len(named))
	for _, n := range named {
		if seen[n] {
			return bad
		}
		seen[n] = true
		if _, ok := pinned[n]; !ok {
			return bad
		}
	}
	for p := range pinned {
		if !seen[p] {
			return bad
		}
	}
	return nil
}
