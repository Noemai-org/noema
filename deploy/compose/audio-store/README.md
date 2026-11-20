# deploy/compose/audio-store

Date: 2026-11-20 · Owner: Integration Lead (IL) · Reviewers: Voice and the Security Reviewer (A451)

**What it is.** The private store for raw audio on the **bench host** (A451, A377 to A379; REQ-VOI-004, AGENT-13): a directory, mode **0700**, owned by one account, with a daily retention purge. Raw audio lives **only** here: **never uploaded**, never in the repository, never in a CI artifact and never mounted into a container this repository starts. Synthetic audio stays labelled as such and is not covered by this folder.

**Who reads it.** The Founders and the voice runner only. Mode 0700 means the owning account alone: make that a dedicated account for the voice runner (the unit below runs as `noema-voice`), and let a Founder act as it (`sudo -u noema-voice`) when they need to listen. There is no group and no world access, and no CI identity ever has any.

**The scripts.**

| File | What it does |
|---|---|
| `store.sh init` | makes the store (mode 0700, yours), or accepts an existing one only if it is already exactly that; it never loosens or tightens a store it did not make |
| `store.sh check` | exit 0 if the store is a directory, not a link, not inside this repository, owned by the caller, mode 700; otherwise 1 and the reason |
| `store.sh purge` | deletes files older than the retention (by each file's own modification time), then empty folders; prints a count, never a name (a file name can hold a speaker); refuses a store that is not right |
| `store.sh remove <speaker_ref>` | deletes one speaker's whole folder, for a withdrawn recording (the plan: within 7 days); refuses a reference that is not a plain name or a link; prints a count, never a name |
| `store.sh scan` | prints the number of objects, the number **without a speaker_ref** (a loose file, a link, or a folder whose name is not a plain reference, and each file in it) and the number **open to group or other** (any 077 permission bit on a file or folder); exit 1 if any of those, or if a link is found inside a speaker folder; counts only, never a name or a content (the plan's "no object without a speaker_ref" check) |
| `noema-audio-purge.service`, `.timer` | systemd units for the host: the daily purge (`Persistent=true`); they read the two settings from `/etc/noema/audio-store.env` (mode 0600), which is not in this repository |

**The layout.** `<store>/<speaker_ref>/...`: every object lives under a speaker reference (a plain name of letters, digits, dot, dash and underscore). Synthetic fixtures live in the same store, labelled SYNTHETIC, and follow the same purge.

**The retention runs from ANALYSIS, not from recording.** The plan (VOI-05, noema#201, section 5; the Founder's decision F3 of 8 Nov, unchanged by A377 to A379) keeps audio for the study and deletes it **90 days after analysis**; a withdrawn recording is deleted within 7 days. So the purge reads each file's own modification time, and **the voice runner sets each file's modification time to the analysis date when the analysis of its session is finished**: "90 days" then runs from analysis. Nobody should read it as "90 days from recording": a file the runner has not yet touched ages from when it was written.

**The two settings, both required, neither with a default (AGENT-16).** `NOEMA_AUDIO_STORE_DIR`: an absolute plain path, outside the checkout and any directory a workflow uploads (for example `/var/lib/noema-voice/audio`: an example only; the path is the Founders' to choose). `NOEMA_AUDIO_RETENTION_DAYS`: a whole number of days, 1 to 9999; **the plan's value is 90**, set on the host in `/etc/noema/audio-store.env`, not in this repository. The live runner receives the path as an argument with no default. The Integrator account's own retention is zero days (A379).

**Encryption at rest is the host's, not this script's.** A Founder (as the host's administrator) provisions the volume encrypted when the host exists; `store.sh` cannot see it and does not claim it. The only agents' access to raw audio is none: the voice runner is a process, not an agent session.

**What the tests prove.** `test_store.sh` (Linux, real directories and modes): both settings are required; a retention of 0, a negative, a leading zero, a unit or a fraction is refused; a relative path, a `..`, a space, a path inside the repository and a link are refused; `init` makes mode 0700 owned by the caller and refuses an existing 0755 store without changing it; `check` and `purge` refuse a store that is not 0700; `purge` keeps recent files, removes old ones and emptied folders, never reads a folder's own time, names nothing, and never follows a link out of the store; `remove` takes one speaker's folder by a plain reference and refuses `..`, `/`, a space, a link and an absent speaker, and names nothing; `scan` counts the objects and those without a speaker_ref, and names nothing. `test_never_leaves_the_host.py`: no workflow or Compose file names either setting, no mount or uploaded path names audio, the units carry no path and no number, the script has no default.

**Needs GNU coreutils** (`stat -c`, GNU `find`): the script says so and stops on a host with another `stat`. The unit has no fixed `ReadWritePaths` (the path is the host's setting) and is hardened instead (no network, no devices, no capabilities, a system-call filter, umask 0077).

**What this does not do.** It does not record, transcribe, move or upload audio, does not install the units, and does not set the Intron account's retention (A379: the Founders). Whether recordings of bystanders are allowed is a recommendation awaiting the Founder (A452): nothing here depends on it.

**Run the tests.** `sh deploy/compose/audio-store/test_store.sh` and `python3 -I deploy/compose/audio-store/test_never_leaves_the_host.py` (both run in `compose-scripts`).

**Implements.** REQ-VOI-004; rulings A451, A377, A378, A379; INV-29 (nothing of this reaches the model service or the core).
