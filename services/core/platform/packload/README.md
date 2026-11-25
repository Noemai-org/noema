# platform/packload

**What it does.** Loads a pack through its manifest and verifies it before any parser sees a byte. `packload.Load(dir, manifest, maxBytes) (Pack, error)` reads the manifest at `manifest` (relative to `dir`) and each file it lists, and refuses the whole pack when anything differs from what the manifest pins. It returns the bytes of the files the manifest lists (the pack, the evidence mapping, the reconciliation rules, and the injection patterns when the manifest lists them) and the ids the manifest expects (`<id>@<version>`); on any refusal it returns nothing. A manifest below version 4 that lists no injection patterns loads, and `Pack.InjectionPatterns` and `Pack.InjectionPatternsID` are then empty: the pack carries no pattern data (A664, interim; the A579 pattern).

**REQs and rulings.** REQ-OPS-003 (packs are data, loaded, never imported); INV-14 (no vertical, folder or file name in core: the manifest names them), INV-21; AGENT-13 (fixed error text), AGENT-16 (the size bound has no default). Ruling A333 (the manifest, hash-pinned now, signatures later) and A320 (one mounted directory). Pack data: `packs/schemas/manifest.v1.json` and `tools/packcheck` (`check_manifest`) are the CI side of the same rules; this package is the runtime side.

**What is refused (each its own error kind, compare with `errors.Is`).**

| Kind | When |
|---|---|
| `ErrConfig` | no directory, a directory that cannot be opened, a bound that is not positive |
| `ErrManifest` | the manifest is missing, over the bound, not JSON, has an unknown, missing or repeated member, a label other than SYNTHETIC, a version below 1, an expected id off its shape, a digest that is not 64 lower-case hex characters, a path listed twice, an unknown kind, not exactly one file of each of the pack, evidence-mapping and rule-set kinds, two injection-pattern files, an injection-pattern file without its expected id or the id without the file, or (from manifest version 4, no opt-in) no injection-pattern file |
| `ErrPath` | a path that is not plain: absolute, `..`, `\`, a drive letter, a hidden or empty component, a control character, over 256 characters |
| `ErrLink` | any component of a path below the directory is a symbolic link, even one that points inside it or at a file with the right digest |
| `ErrFile` | a listed file is missing, is not a regular file, or is over the bound |
| `ErrHash` | a listed file's SHA-256 is not the one the manifest pins (a file changed after CI) |
| `ErrUnlisted` | a file of a listed kind is not listed: a second mapping or rule set beside the pinned one (any file of the same extension in the listed file's folder, at any depth, links included and not followed), or a second pack file of the same family in the pack directory (`x_v1.json` and `x_v2.json`) |
| `ErrID` | the mapping, the rule set or the injection patterns do not carry the id the manifest expects, or the mapping was written for another rule set |

The error text is fixed: it names the kind of refusal and, from a closed set (`manifest`, `pack`, `evidence_mapping`, `recon_rules`, `injection_patterns`, `action_graph`), which file. It never holds a path, a digest, a file's content or an operating-system error; the kind of refusal is reachable with `errors.Is` and the file kind with `errors.As(err, &*packload.Error)`.

**How it reads.** Through `os.Root`, so a component that is swapped for a link after the check still cannot lead outside the directory. Each component below the directory is examined with `Lstat` before the file is opened. The directory itself is the caller's mount and is not examined for a link. Each file is read at most `maxBytes + 1` bytes: one byte over is refused.

**Where a kind lives is not this package's to know.** The manifest decides; the unlisted-file rule is read from where the manifest put each listed file. A kind in a folder of its own owns every file of that extension below it; a kind in the pack directory itself owns the files of its family. The manifest itself is not a file of any kind.

**Does not.** Parse a pack (the Understanding gate, the rule set and the evidence mapping parsers do, from the bytes returned here), check where a kind belongs (`tools/packcheck` does), verify a signature (A320: signatures later), import a pack as code (SoT 51), hold a default.

**Status.** Built (UND-04 wiring slice 1); used by `services/core/cmd/core/wire_understanding.go`.

**Tests.** `go test -count=1 ./platform/packload/` from `services/core/`. Real files in a real directory, nothing mocked: a byte changed after the manifest was made, a symlink in each position, a stray second mapping, a path of every escaping spelling.

**Action graphs (A510).** The manifest may list files of kind `action_graph`. `Load` verifies each (path rules, no link, SHA-256) and returns the verified bytes in `Pack.ActionGraphs`, by the pinned path. The pinned graphs must be exactly the ones the verified pack file names in `action_graph_files` (a pack that names none pins none): any difference is `ErrGraph` (kind `action_graph`), and a `*.json` of the graph folder that is not listed is `ErrUnlisted`. A refused pack returns no graph. The loader holds no graph path or id; the pack and the manifest name them (INV-14).
