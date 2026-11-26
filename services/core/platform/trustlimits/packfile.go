package trustlimits

import "bytes"

// LoadPackFile is the start-up validation of the pack's trust-limits file (A612, interim; AGENT-15, AGENT-16, INV-14). files is the verified Pack.Limits map (the bytes the manifest
// pinned, by pinned path) and path is the one the composition names: this package, like the core, names no pack file. The bytes go through Load, so a file that is not exactly
// trust-limits.v2 (malformed, a v1 document, a v2 file without the ceiling, an unknown member, another pack's) is a *Error and the returned Limits is the zero value; a path that
// is empty or not pinned is a *Error as well. The refusal never carries the path or the document, so a start-up log line cannot leak either. The composition refuses to start on an error.
func LoadPackFile(files map[string][]byte, path, wantPackID string) (Limits, error) {
	if path == "" {
		return Limits{}, refuse("no limits file is named", nil)
	}
	b, ok := files[path]
	if !ok {
		return Limits{}, refuse("the named limits file is not one the manifest pins", nil)
	}
	return Load(bytes.NewReader(b), wantPackID)
}
