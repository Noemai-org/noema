package trustpack

import (
	"bytes"
	"io/fs"
	"path"
	"time"
)

// verifiedPackName is the name of the pack file inside the in-memory root of LoadVerified. A manifest path never starts with a dot (packload's path rule), so no graph can have it.
const verifiedPackName = ".pack"

// LoadVerified builds the Pack from bytes that were verified elsewhere (platform/packload: the manifest pins the pack file and every action graph it names, A333, A510): the pack
// file's bytes, and the graphs by the path the pack names them. It reads nothing else, no directory and no second copy of a file, so the graphs it serves are the ones that were
// verified (INV-16, INV-21). It refuses, with ErrPack, a graph the pack names that `graphs` lacks and a graph in `graphs` that the pack does not name (nobody checked it against this
// pack); with ErrConfig, no pack bytes. Every other refusal is LoadPack's.
func LoadVerified(pack []byte, graphs map[string][]byte) (*Pack, error) {
	if len(pack) == 0 {
		return nil, ErrConfig
	}
	root := &verifiedFS{files: map[string][]byte{verifiedPackName: pack}, read: map[string]bool{}}
	for name, data := range graphs {
		if name == verifiedPackName {
			return nil, packErr("a graph has the name of the pack file")
		}
		root.files[name] = data
	}
	p, err := LoadPack(root, verifiedPackName)
	if err != nil {
		return nil, err
	}
	for name := range graphs {
		if !root.read[name] {
			return nil, packErr("a graph is given that the pack does not name")
		}
	}
	return p, nil
}

// Ref is `<pack id>@<pack version>` exactly as the pack file carries them: the value of TrustAdapters.PackVersion and the same one the Interpreter port is given (A377 item 6). A nil
// pack and a zero Pack have none.
func (p *Pack) Ref() string {
	if p == nil {
		return ""
	}
	return p.ref
}

// verifiedFS is a read-only file system over bytes, and it remembers which of them were read. It exists so that LoadPack, which reads by name, can be given the verified bytes and nothing else.
type verifiedFS struct {
	files map[string][]byte
	read  map[string]bool
}

func (v *verifiedFS) Open(name string) (fs.File, error) {
	data, ok := v.files[name]
	if !fs.ValidPath(name) || !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	v.read[name] = true
	return &verifiedFile{Reader: bytes.NewReader(data), info: verifiedInfo{name: path.Base(name), size: int64(len(data))}}, nil
}

type verifiedFile struct {
	*bytes.Reader
	info verifiedInfo
}

func (f *verifiedFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *verifiedFile) Close() error               { return nil }

type verifiedInfo struct {
	name string
	size int64
}

func (i verifiedInfo) Name() string       { return i.name }
func (i verifiedInfo) Size() int64        { return i.size }
func (i verifiedInfo) Mode() fs.FileMode  { return 0o444 }
func (i verifiedInfo) ModTime() time.Time { return time.Time{} }
func (i verifiedInfo) IsDir() bool        { return false }
func (i verifiedInfo) Sys() any           { return nil }

// LoadVerifiedWithLimits is LoadVerified that also keeps the verified bytes of the limits files the manifest pins, by the path it pins (A544, A561): the one source of the pack's
// limits for whoever composes the process, so no limits file is read a second time (INV-16). The bytes are copied: the caller's map and the Pack's share nothing. What a limits file
// means is the caller's (platform/trustlimits); this package only holds the verified bytes.
func LoadVerifiedWithLimits(pack []byte, graphs, limits map[string][]byte) (*Pack, error) {
	p, err := LoadVerified(pack, graphs)
	if err != nil {
		return nil, err
	}
	p.limits = copyFiles(limits)
	return p, nil
}

// LimitsFiles is a copy of the verified limits files LoadVerifiedWithLimits kept, by pinned path; nil when none were given and for a nil Pack.
func (p *Pack) LimitsFiles() map[string][]byte {
	if p == nil {
		return nil
	}
	return copyFiles(p.limits)
}

func copyFiles(in map[string][]byte) map[string][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(in))
	for name, b := range in {
		out[name] = bytes.Clone(b)
	}
	return out
}
