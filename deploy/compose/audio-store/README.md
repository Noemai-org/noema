# deploy/compose/audio-store

Date: 2026-11-20 · Owner: Integration Lead (IL) · Reviewers: Voice and the Security Reviewer (A451)

**What it is.** The private store for raw audio on the **bench host** (A451, A377 to A379; REQ-VOI-004, AGENT-13): a directory, mode **0700**, owned by one account, with a daily retention purge. Raw audio lives **only** here: **never uploaded**, never in the repository, never in a CI artifact and never mounted into a container this repository starts. Synthetic audio stays labelled as such and is not covered by this folder.

**Who reads it.** The Founders and the voice runner only. Mode 0700 means the owning account alone: make that a dedicated account for the voice runner (the unit below runs as `noema-voice`), and let a Founder act as it (`sudo -u noema-voice`) when they need to listen. There is no group and no world access, and no CI identity ever has any.

**The scripts.**

| File | What it does |
|---|---|
| `store.sh init` | makes the store (mode 0700, yours), or accepts an existing one only if it is already exactly that; it never loosens or tightens a store it did not make |
| `store.sh check` | exit 0 if the store is a directory, not a link, not inside this repository, owned by the caller, mode 700; otherwise 1 and the reason |
| `store.sh purge` | deletes files older than the retention (by modification time), then empty folders; prints a count, never a name (a file name can hold a speaker); refuses a store that is not right |
| `noema-audio-purge.service`, `.timer` | systemd units for the host: the daily purge (`Persistent=true`); they read the two settings from `/etc/noema/audio-store.env` (mode 0600), which is not in this repository |

**The two settings, both required, neither with a default (AGENT-16).** `NOEMA_AUDIO_STORE_DIR`: an absolute plain path, outside the checkout. `NOEMA_AUDIO_RETENTION_DAYS`: a whole number of days, 1 to 9999. **The retention number is the VOI-05 plan's, not this repository's**: the plan row says "a retention timer" and the Integrator account's own retention is zero days (A379); until Voice or the Founders state the number, nothing here purges, because the script refuses to run without it.

**What the tests prove.** `test_store.sh` (Linux, real directories and modes): both settings are required; a retention of 0, a negative, a leading zero, a unit or a fraction is refused; a relative path, a `..`, a space, a path inside the repository and a link are refused; `init` makes mode 0700 owned by the caller and refuses an existing 0755 store without changing it; `check` and `purge` refuse a store that is not 0700; `purge` keeps recent files, removes old ones and emptied folders, names nothing, and never follows a link out of the store. `test_never_leaves_the_host.py`: no workflow or Compose file names either setting, no mount or uploaded path names audio, the units carry no path and no number, the script has no default.

**What this does not do.** It does not record, transcribe, move or upload audio, does not install the units, and does not set the Intron account's retention (A379: the Founders). Whether recordings of bystanders are allowed is a recommendation awaiting the Founder (A452): nothing here depends on it.

**Run the tests.** `sh deploy/compose/audio-store/test_store.sh` and `python3 -I deploy/compose/audio-store/test_never_leaves_the_host.py` (both run in `compose-scripts`).

**Implements.** REQ-VOI-004; rulings A451, A377, A378, A379; INV-29 (nothing of this reaches the model service or the core).
