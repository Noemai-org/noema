package packload_test

// UND-04 wiring slice 1 (rulings A320 and A333; REQ-OPS-003; INV-14, INV-21; AGENT-13, AGENT-16): the core loads the pack through its manifest and refuses the whole pack when
// anything differs from what the manifest pins. Every fault here is a real file in a real directory (a byte changed after the manifest was made, a symlink, a stray file); nothing is
// mocked. The packs are SYNTHETIC and made per test; they name no vertical the core knows.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

const (
	manifestRel = "demo/manifest.v1.json"
	packRel     = "demo_v1.json"
	mapRel      = "demo/evidence_mapping/mapping.v1.json"
	rulesRel    = "demo/recon_rules/rules.v1.json"
	bound       = int64(1 << 16)

	packBody  = `{"pack_id":"demo","pack_version":"1.0.0"}`
	mapBody   = `{"mapping_id":"demo.map.v1","version":1,"ruleset":"demo.rules@1"}`
	rulesBody = `{"ruleset_id":"demo.rules","version":1}`
)

// A645: the injection patterns are the fourth pinned file of every pack.
const (
	patternsRel  = "demo/context/injection_patterns.v1.json"
	patternsBody = `{"pack_id":"demo.ctx.injection_patterns","version":1,"label":"SYNTHETIC","note":"n","patterns":[]}`
	patternsID   = "demo.ctx.injection_patterns@1"
)

// world is one pack directory and the manifest that pins it.
type world struct {
	t    *testing.T
	dir  string
	exp  map[string]string // the manifest's `expected`
	list []entry           // the manifest's files, in order
}

type entry struct{ Kind, Path, Sha string }

func sum(b string) string { s := sha256.Sum256([]byte(b)); return hex.EncodeToString(s[:]) }

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, dir: t.TempDir(), exp: map[string]string{"evidence_mapping_id": "demo.map.v1@1", "recon_rule_set_id": "demo.rules@1", "injection_patterns_id": patternsID}}
	w.write(packRel, packBody)
	w.write(mapRel, mapBody)
	w.write(rulesRel, rulesBody)
	w.write(patternsRel, patternsBody)
	w.list = []entry{{"pack", packRel, sum(packBody)}, {"evidence_mapping", mapRel, sum(mapBody)}, {"recon_rules", rulesRel, sum(rulesBody)}, {"injection_patterns", patternsRel, sum(patternsBody)}}
	w.seal()
	return w
}

func (w *world) write(rel, body string) {
	w.t.Helper()
	p := filepath.Join(w.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

// seal writes the manifest from the current list and expected ids, as CI's `sha256sum` step would.
func (w *world) seal() {
	w.t.Helper()
	files := make([]map[string]string, 0, len(w.list))
	for _, e := range w.list {
		files = append(files, map[string]string{"kind": e.Kind, "path": e.Path, "sha256": e.Sha})
	}
	doc := map[string]any{"manifest_id": "demo.manifest", "version": 1, "label": "SYNTHETIC", "note": "synthetic", "expected": w.exp, "files": files}
	b, err := json.Marshal(doc)
	if err != nil {
		w.t.Fatal(err)
	}
	w.write(manifestRel, string(b))
}

// resealed rewrites the digest of every listed file from what is on disk now, so that only the ids and the structure are left to differ.
func (w *world) resealed() *world {
	w.t.Helper()
	for i, e := range w.list {
		b, err := os.ReadFile(filepath.Join(w.dir, filepath.FromSlash(e.Path)))
		if err == nil {
			w.list[i].Sha = sum(string(b))
		}
	}
	w.seal()
	return w
}

func (w *world) load() (packload.Pack, error) { return packload.Load(w.dir, manifestRel, bound) }

func wantErr(t *testing.T, what string, err error, kind error, p packload.Pack) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Errorf("%s: got %v; want %v", what, err, kind)
	}
	if len(p.Pack)+len(p.EvidenceMapping)+len(p.ReconRules) != 0 || p.EvidenceMappingID != "" || p.ReconRuleSetID != "" {
		t.Errorf("%s: a refused pack returned content: %+v", what, p)
	}
}

// REQ-OPS-003: a pack whose files are exactly what the manifest pins loads, with the bytes the digests cover and the ids the manifest names.
func TestAVerifiedPackLoads(t *testing.T) {
	p, err := newWorld(t).load()
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Pack) != packBody || string(p.EvidenceMapping) != mapBody || string(p.ReconRules) != rulesBody {
		t.Errorf("the bytes are not the files': %+v", p)
	}
	if p.EvidenceMappingID != "demo.map.v1@1" || p.ReconRuleSetID != "demo.rules@1" {
		t.Errorf("ids %q, %q", p.EvidenceMappingID, p.ReconRuleSetID)
	}
}

// A333 acceptance: a pack file changed after the manifest was made stops the load, whichever of the three it is, even by one byte.
func TestAPackFileChangedAfterTheManifestStopsTheLoad(t *testing.T) {
	for _, c := range []struct{ name, rel, body string }{
		{"the pack", packRel, strings.Replace(packBody, "demo", "demp", 1)},
		{"the evidence mapping", mapRel, mapBody + " "},
		{"the rule set", rulesRel, strings.Replace(rulesBody, `"version":1`, `"version":2`, 1)},
	} {
		w := newWorld(t)
		w.write(c.rel, c.body)
		p, err := w.load()
		wantErr(t, c.name, err, packload.ErrHash, p)
	}
}

// The digest is the file's raw bytes: a changed file whose digest was left in the manifest in upper case or truncated is a manifest the shape refuses, not a match.
func TestADigestThatIsNotSixtyFourLowerCaseHexIsRefused(t *testing.T) {
	for name, bad := range map[string]string{"upper case": strings.ToUpper(sum(packBody)), "short": sum(packBody)[:63], "long": sum(packBody) + "0", "not hex": strings.Repeat("g", 64), "empty": ""} {
		w := newWorld(t)
		w.list[0].Sha = bad
		w.seal()
		p, err := w.load()
		wantErr(t, name, err, packload.ErrManifest, p)
	}
}

// A333 acceptance: an id other than the manifest's is refused, in the mapping, in the rule set, in the version and in the mapping's own reference to its rule set.
func TestAnIdOtherThanTheManifestsIsRefused(t *testing.T) {
	cases := map[string]func(w *world){
		"the mapping id":       func(w *world) { w.exp["evidence_mapping_id"] = "demo.map.v2@1" },
		"the mapping version":  func(w *world) { w.exp["evidence_mapping_id"] = "demo.map.v1@2" },
		"the rule set id":      func(w *world) { w.exp["recon_rule_set_id"] = "demo.other@1" },
		"the rule set version": func(w *world) { w.exp["recon_rule_set_id"] = "demo.rules@2" },
		"the file's mapping id": func(w *world) {
			w.write(mapRel, strings.Replace(mapBody, "demo.map.v1", "demo.map.v9", 1))
		},
		"the file's rule set id": func(w *world) {
			w.write(rulesRel, strings.Replace(rulesBody, "demo.rules", "demo.rulez", 1))
		},
		"the file's version": func(w *world) { w.write(mapRel, strings.Replace(mapBody, `"version":1`, `"version":3`, 1)) },
		"the mapping's rule set": func(w *world) {
			w.write(mapRel, strings.Replace(mapBody, "demo.rules@1", "demo.rules@2", 1))
		},
	}
	for name, mutate := range cases {
		w := newWorld(t)
		mutate(w)
		p, err := w.resealed().load()
		wantErr(t, name, err, packload.ErrID, p)
	}
}

// An id the file does not carry in the right types is not an id: a version that is a string or a float, a missing member, a boolean.
func TestAFileWithNoUsableIdIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"a string version":       `{"mapping_id":"demo.map.v1","version":"1","ruleset":"demo.rules@1"}`,
		"a float version":        `{"mapping_id":"demo.map.v1","version":1.5,"ruleset":"demo.rules@1"}`,
		"no id":                  `{"version":1,"ruleset":"demo.rules@1"}`,
		"a boolean version":      `{"mapping_id":"demo.map.v1","version":true,"ruleset":"demo.rules@1"}`,
		"not an object":          `[1]`,
		"not JSON":               `demo`,
		"an id that is a number": `{"mapping_id":7,"version":1,"ruleset":"demo.rules@1"}`,
	} {
		w := newWorld(t)
		w.write(mapRel, body)
		p, err := w.resealed().load()
		wantErr(t, name, err, packload.ErrID, p)
	}
}

// A path that escapes the directory is refused before anything is read, in every spelling, and the error never repeats the path (AGENT-13).
func TestAPathThatEscapesTheDirectoryIsRefused(t *testing.T) {
	outside := filepath.Join(filepath.Dir(t.TempDir()), "outside.json")
	for name, rel := range map[string]string{
		"absolute":            outside,
		"a leading slash":     "/etc/passwd",
		"a parent component":  "demo/../../outside.json",
		"a parent only":       "..",
		"a backslash":         `demo\evidence_mapping\mapping.v1.json`,
		"a leading backslash": `\demo`,
		"a drive letter":      `C:demo.json`,
		"a hidden component":  "demo/.hidden/mapping.v1.json",
		"a hidden file":       ".demo_v1.json",
		"a dot component":     "demo/./mapping.v1.json",
		"an empty component":  "demo//mapping.v1.json",
		"a trailing slash":    "demo/mapping.v1.json/",
		"a home shortcut":     "~/demo.json",
		"a newline":           "demo/mapping\n.json",
		"a control character": "demo/\x1b[31mevil.json",
		"empty":               "",
		"a NUL":               "demo\x00.json",
		"too long":            strings.Repeat("a", 257),
	} {
		w := newWorld(t)
		w.list[1].Path = rel
		w.seal()
		p, err := w.load()
		wantErr(t, name, err, packload.ErrPath, p)
		if err != nil && (strings.Contains(err.Error(), "evil") || strings.Contains(err.Error(), "passwd") || strings.Contains(err.Error(), "outside")) {
			t.Errorf("%s: the error repeats the path: %v", name, err)
		}
	}
}

// The manifest's own path follows the same rules.
func TestTheManifestPathIsPlainToo(t *testing.T) {
	w := newWorld(t)
	for _, rel := range []string{"../manifest.v1.json", "/demo/manifest.v1.json", `demo\manifest.v1.json`, ".demo/manifest.v1.json", ""} {
		p, err := packload.Load(w.dir, rel, bound)
		wantErr(t, "manifest path "+rel, err, packload.ErrPath, p)
	}
}

// A symlink anywhere in the path is refused, even one that points at a file with the right digest inside the directory: a pinned file is a plain file.
func TestASymlinkInAnyComponentIsRefused(t *testing.T) {
	cases := map[string]func(w *world){
		"the pack file itself": func(w *world) {
			w.write("real_v1.json", packBody)
			must(t, os.Remove(filepath.Join(w.dir, packRel)))
			must(t, os.Symlink(filepath.Join(w.dir, "real_v1.json"), filepath.Join(w.dir, packRel)))
		},
		"a relative link to a sibling": func(w *world) {
			w.write("real_v1.json", packBody)
			must(t, os.Remove(filepath.Join(w.dir, packRel)))
			must(t, os.Symlink("real_v1.json", filepath.Join(w.dir, packRel)))
		},
		"a directory component": func(w *world) {
			must(t, os.Rename(filepath.Join(w.dir, "demo", "recon_rules"), filepath.Join(w.dir, "demo", "rules_real")))
			must(t, os.Symlink("rules_real", filepath.Join(w.dir, "demo", "recon_rules")))
		},
		"the top directory component": func(w *world) {
			must(t, os.Rename(filepath.Join(w.dir, "demo"), filepath.Join(w.dir, "demo_real")))
			must(t, os.Symlink("demo_real", filepath.Join(w.dir, "demo")))
		},
		"a link that leaves the directory": func(w *world) {
			out := filepath.Join(t.TempDir(), "elsewhere.json")
			must(t, os.WriteFile(out, []byte(mapBody), 0o644))
			must(t, os.Remove(filepath.Join(w.dir, mapRel)))
			must(t, os.Symlink(out, filepath.Join(w.dir, mapRel)))
		},
	}
	for name, mutate := range cases {
		w := newWorld(t)
		mutate(w)
		p, err := w.load()
		wantErr(t, name, err, packload.ErrLink, p)
	}
}

// A manifest reached through a link is not read.
func TestAManifestThatIsASymlinkIsNotRead(t *testing.T) {
	w := newWorld(t)
	must(t, os.Rename(filepath.Join(w.dir, manifestRel), filepath.Join(w.dir, "demo", "real.json")))
	must(t, os.Symlink("real.json", filepath.Join(w.dir, manifestRel)))
	p, err := w.load()
	wantErr(t, "manifest link", err, packload.ErrLink, p)
}

// A file that is missing, a directory where a file should be, and a file over the bound are each ErrFile; a file exactly at the bound loads.
func TestAFileThatIsMissingNotPlainOrTooLargeIsRefused(t *testing.T) {
	w := newWorld(t)
	must(t, os.Remove(filepath.Join(w.dir, mapRel)))
	p, err := w.load()
	wantErr(t, "missing", err, packload.ErrFile, p)

	w = newWorld(t)
	must(t, os.Remove(filepath.Join(w.dir, rulesRel)))
	must(t, os.Mkdir(filepath.Join(w.dir, rulesRel), 0o755))
	p, err = w.load()
	wantErr(t, "a directory", err, packload.ErrFile, p)

	// The bound is the manifest's own size, so a pack file one byte longer is over it and one of exactly that size is not.
	w = newWorld(t)
	manifest, rerr := os.ReadFile(filepath.Join(w.dir, manifestRel))
	must(t, rerr)
	size := int64(len(manifest))
	pad := func(n int64) string {
		return `{"pack_id":"demo","pack_version":"1.0.0","pad":"` + strings.Repeat("x", int(n)-len(packBody)-9) + `"}`
	}
	w.write(packRel, pad(size+1))
	p, err = packload.Load(w.dir, manifestRel, size)
	wantErr(t, "one byte over the bound", err, packload.ErrFile, p)
	w.write(packRel, pad(size))
	w.resealed()
	if _, err := packload.Load(w.dir, manifestRel, size); err != nil {
		t.Errorf("every file at most the bound: %v", err)
	}
}

// The manifest is bounded too: one byte over is refused before it is decoded.
func TestTheManifestIsBounded(t *testing.T) {
	w := newWorld(t)
	manifest, err := os.ReadFile(filepath.Join(w.dir, manifestRel))
	must(t, err)
	p, err := packload.Load(w.dir, manifestRel, int64(len(manifest))-1)
	wantErr(t, "an oversize manifest", err, packload.ErrManifest, p)
}

// A file of a listed kind that the manifest does not list is a second candidate for the same role, and the load stops: a second mapping beside the pinned one, in the same folder, in
// a folder below it, as a link, or a second pack file of the same family. Unrelated files beside them are not the manifest's business.
func TestAFileOfAListedKindThatIsNotListedIsRefused(t *testing.T) {
	cases := map[string]func(w *world){
		"a second mapping":       func(w *world) { w.write("demo/evidence_mapping/mapping.v2.json", mapBody) },
		"a second mapping below": func(w *world) { w.write("demo/evidence_mapping/old/mapping.v0.json", mapBody) },
		"a second rule set":      func(w *world) { w.write("demo/recon_rules/other.json", rulesBody) },
		"a link named like a mapping": func(w *world) {
			must(t, os.Symlink("mapping.v1.json", filepath.Join(w.dir, "demo/evidence_mapping/alias.json")))
		},
		"a dangling link":                   func(w *world) { must(t, os.Symlink("nowhere", filepath.Join(w.dir, "demo/recon_rules/ghost.json"))) },
		"a second pack file":                func(w *world) { w.write("demo_v2.json", packBody) },
		"a second pack file, another minor": func(w *world) { w.write("demo_v1.1.json", packBody) },
	}
	for name, mutate := range cases {
		w := newWorld(t)
		mutate(w)
		p, err := w.load()
		wantErr(t, name, err, packload.ErrUnlisted, p)
	}
	w := newWorld(t)
	w.write("README.md", "# synthetic")
	w.write("other_v1.json", packBody)    // another family of the pack folder
	w.write("demo/notes.txt", "not data") // not a file of a listed kind
	w.write("demo/evidence_mapping/README.md", "# synthetic")
	if _, err := w.load(); err != nil {
		t.Errorf("files that are not of a listed kind refused the load: %v", err)
	}
}

// The manifest's shape is closed: every member required, none unknown, none repeated, exactly one file of each kind, no path twice, at most sixteen files, the label SYNTHETIC.
func TestAManifestThatIsNotExactlyItsShapeIsRefused(t *testing.T) {
	good := func(w *world) string { b, _ := os.ReadFile(filepath.Join(w.dir, manifestRel)); return string(b) }
	entryJSON := func(e entry) string { return fmt.Sprintf(`{"kind":%q,"path":%q,"sha256":%q}`, e.Kind, e.Path, e.Sha) }
	cases := map[string]func(w *world) string{
		"not JSON":                func(*world) string { return "demo" },
		"empty":                   func(*world) string { return "" },
		"an array":                func(*world) string { return "[]" },
		"data after the document": func(w *world) string { return good(w) + "{}" },
		"an unknown member":       func(w *world) string { return strings.Replace(good(w), `"version":1`, `"version":1,"extra":1`, 1) },
		"a repeated member":       func(w *world) string { return strings.Replace(good(w), `"version":1`, `"version":1,"version":1`, 1) },
		"a repeated digest": func(w *world) string {
			return strings.Replace(good(w), `"sha256":"`+sum(packBody)+`"`, `"sha256":"`+strings.Repeat("0", 64)+`","sha256":"`+sum(packBody)+`"`, 1)
		},
		"a repeated expected id": func(w *world) string {
			return strings.Replace(good(w), `"evidence_mapping_id":"demo.map.v1@1"`, `"evidence_mapping_id":"x.y@1","evidence_mapping_id":"demo.map.v1@1"`, 1)
		},
		"the wrong label": func(w *world) string { return strings.Replace(good(w), `"SYNTHETIC"`, `"VALIDATED"`, 1) },
		"a zero version":  func(w *world) string { return strings.Replace(good(w), `"version":1`, `"version":0`, 1) },
		"no label":        func(w *world) string { return strings.Replace(good(w), `"label":"SYNTHETIC",`, ``, 1) },
		"no expected": func(w *world) string {
			return strings.Replace(good(w), `"expected":{"evidence_mapping_id":"demo.map.v1@1","injection_patterns_id":"demo.ctx.injection_patterns@1","recon_rule_set_id":"demo.rules@1"},`, ``, 1)
		},
		"no files":                           func(w *world) string { return strings.Replace(good(w), `"files":[`, `"files":[],"x":[`, 1) },
		"an expected id off its shape":       func(w *world) string { return strings.Replace(good(w), `demo.map.v1@1`, `Demo.map.v1@1`, 1) },
		"an expected id with no version":     func(w *world) string { return strings.Replace(good(w), `"demo.rules@1"`, `"demo.rules"`, 1) },
		"an expected id with a zero version": func(w *world) string { return strings.Replace(good(w), `"demo.rules@1"`, `"demo.rules@0"`, 1) },
		"an unknown kind":                    func(w *world) string { return strings.Replace(good(w), `"kind":"recon_rules"`, `"kind":"graphs"`, 1) },
		"a missing kind":                     func(w *world) string { return strings.Replace(good(w), `"kind":"recon_rules"`, `"kind":"pack"`, 1) },
		"a path twice": func(w *world) string {
			w.list[1].Path = packRel // two kinds, one file: only the repeated path is wrong
			w.list[1].Sha = w.list[0].Sha
			w.seal()
			return good(w)
		},
		"no pack file":     func(w *world) string { w.list = w.list[1:]; w.seal(); return good(w) },
		"no mapping file":  func(w *world) string { w.list = []entry{w.list[0], w.list[2]}; w.seal(); return good(w) },
		"no rule set file": func(w *world) string { w.list = w.list[:2]; w.seal(); return good(w) },
		"a fourth file of a kind nobody knows": func(w *world) string {
			w.write("demo/extra.json", "{}")
			w.list = append(w.list, entry{"graphs", "demo/extra.json", sum("{}")})
			w.seal()
			return good(w)
		},
		"an empty note": func(w *world) string { return strings.Replace(good(w), `"note":"synthetic"`, `"note":""`, 1) },
		"a note over 4000 characters": func(w *world) string {
			return strings.Replace(good(w), `"note":"synthetic"`, `"note":"`+strings.Repeat("n", 4001)+`"`, 1)
		},
		"an expected id over 128 characters": func(w *world) string {
			return strings.Replace(good(w), `"demo.rules@1"`, `"`+strings.Repeat("a", 127)+`@1"`, 1)
		},
		"a manifest id off its shape": func(w *world) string { return strings.Replace(good(w), `"demo.manifest"`, `"Demo"`, 1) },
		"two files of a kind": func(w *world) string {
			w.write("demo/recon_rules/rules.v2.json", rulesBody)
			w.list = append(w.list, entry{"recon_rules", "demo/recon_rules/rules.v2.json", sum(rulesBody)})
			w.seal()
			return good(w)
		},
		"seventeen files": func(w *world) string {
			parts := []string{entryJSON(w.list[0]), entryJSON(w.list[1]), entryJSON(w.list[2])}
			for i := 0; i < 14; i++ {
				parts = append(parts, entryJSON(w.list[2]))
			}
			return strings.Replace(good(w), good(w)[strings.Index(good(w), `"files":[`):], `"files":[`+strings.Join(parts, ",")+`]}`, 1)
		},
		"an entry with an unknown member": func(w *world) string {
			return strings.Replace(good(w), `{"kind":"pack"`, `{"extra":1,"kind":"pack"`, 1)
		},
		"an entry with no digest": func(w *world) string {
			return strings.Replace(good(w), `,"sha256":"`+sum(packBody)+`"`, ``, 1)
		},
	}
	for name, mutate := range cases {
		w := newWorld(t)
		body := mutate(w)
		w.write(manifestRel, body)
		p, err := w.load()
		wantErr(t, name, err, packload.ErrManifest, p)
	}
}

// No count of files, no file name and no id is the core's: the same code loads a pack of any name (INV-14). Here the pack, the folders and the ids are renamed throughout.
func TestAnyNamesLoadBecauseTheManifestNamesThem(t *testing.T) {
	dir := t.TempDir()
	put := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(body), 0o644))
	}
	pk, mp, rs := `{"x":1}`, `{"mapping_id":"zz.m","version":4,"ruleset":"zz.r@9"}`, `{"ruleset_id":"zz.r","version":9}`
	put("zz_v7.json", pk)
	put("zz/maps/m.json", mp)
	put("zz/rules/r.json", rs)
	ip := `{"pack_id":"zz.inj","version":3}`
	put("zz/ctx/i.json", ip)
	manifest := fmt.Sprintf(`{"manifest_id":"zz.manifest","version":2,"label":"SYNTHETIC","expected":{"evidence_mapping_id":"zz.m@4","recon_rule_set_id":"zz.r@9","injection_patterns_id":"zz.inj@3"},"files":[`+
		`{"kind":"recon_rules","path":"zz/rules/r.json","sha256":%q},{"kind":"pack","path":"zz_v7.json","sha256":%q},{"kind":"evidence_mapping","path":"zz/maps/m.json","sha256":%q},`+
		`{"kind":"injection_patterns","path":"zz/ctx/i.json","sha256":%q}]}`, sum(rs), sum(pk), sum(mp), sum(ip))
	put("zz/m.json", manifest)
	p, err := packload.Load(dir, "zz/m.json", bound)
	if err != nil || string(p.Pack) != pk || string(p.EvidenceMapping) != mp || string(p.ReconRules) != rs || p.EvidenceMappingID != "zz.m@4" || p.ReconRuleSetID != "zz.r@9" {
		t.Fatalf("got %+v, %v", p, err)
	}
}

// The configuration is checked before any file is read: no directory, a directory that does not exist, a file for a directory, and a bound that is not positive.
func TestAnUnusableConfigurationIsRefused(t *testing.T) {
	w := newWorld(t)
	for name, c := range map[string]struct {
		dir, manifest string
		max           int64
	}{
		"no directory":        {"", manifestRel, bound},
		"a missing directory": {filepath.Join(w.dir, "nowhere"), manifestRel, bound},
		"a file as directory": {filepath.Join(w.dir, packRel), manifestRel, bound},
		"a zero bound":        {w.dir, manifestRel, 0},
		"a negative bound":    {w.dir, manifestRel, -1},
	} {
		p, err := packload.Load(c.dir, c.manifest, c.max)
		wantErr(t, name, err, packload.ErrConfig, p)
	}
}

// AGENT-13: no refusal repeats a file's content, a digest or the directory.
func TestNoRefusalRepeatsContentDigestOrDirectory(t *testing.T) {
	w := newWorld(t)
	w.write(mapRel, mapBody+" ")
	_, err := w.load()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, leak := range []string{sum(mapBody), mapBody, w.dir, "mapping.v1"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error repeats %q: %v", leak, err)
		}
	}
	if !strings.Contains(err.Error(), "evidence_mapping") {
		t.Errorf("the error does not name the kind of file (a closed set): %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// The manifest is not a file of any kind, even where it sits beside a listed file or below its folder.
func TestTheManifestIsNotOfAnyKind(t *testing.T) {
	w := newWorld(t)
	must(t, os.Rename(filepath.Join(w.dir, rulesRel), filepath.Join(w.dir, "demo", "rules.v1.json")))
	w.list[2].Path = "demo/rules.v1.json"
	w.seal() // demo/manifest.v1.json is now in the folder of a listed file
	if _, err := w.load(); err != nil {
		t.Errorf("the manifest was taken for a second file of a kind: %v", err)
	}
}

// A note of exactly 4000 characters and an expected id of exactly 128 are inside the manifest's bounds.
func TestTheManifestBoundsAreInclusive(t *testing.T) {
	w := newWorld(t)
	b, err := os.ReadFile(filepath.Join(w.dir, manifestRel))
	must(t, err)
	w.write(manifestRel, strings.Replace(string(b), `"note":"synthetic"`, `"note":"`+strings.Repeat("n", 4000)+`"`, 1))
	if _, err := packload.Load(w.dir, manifestRel, 1<<20); err != nil {
		t.Errorf("a note of 4000 characters: %v", err)
	}
	long := strings.Repeat("a", 126) + "@1" // 128 characters
	w = newWorld(t)
	w.exp["recon_rule_set_id"] = long
	w.write(rulesRel, strings.Replace(rulesBody, "demo.rules", strings.Repeat("a", 126), 1))
	w.write(mapRel, strings.Replace(mapBody, "demo.rules@1", long, 1))
	if _, err := w.resealed().load(); err != nil {
		t.Errorf("an expected id of 128 characters: %v", err)
	}
}
