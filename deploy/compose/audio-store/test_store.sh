#!/bin/sh
# Tests for store.sh, the private raw-audio store of the bench host (A451, A377 to A379; REQ-VOI-004). Real directories and real modes, run as the runner's own user. SYNTHETIC: the files hold
# the word "synthetic", never audio. Written before store.sh exists (the first run fails because there is no store.sh).
set -u
here="$(cd "$(dirname "$0")" && pwd)"
store="$here/store.sh"
fail() { echo "audio-store test: $1"; exit 1; }
[ -f "$store" ] || fail "store.sh does not exist"
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
chmod 755 "$work"
run() { # args: command; the environment is the caller's
  sh "$store" "$@" 2>&1
}
env_run() { # STORE RETENTION command
  NOEMA_AUDIO_STORE_DIR="$1" NOEMA_AUDIO_RETENTION_DAYS="$2" sh "$store" "$3" 2>&1
}
good="$work/store"

# Nothing is read from a default: both settings are required.
out="$(env -u NOEMA_AUDIO_STORE_DIR -u NOEMA_AUDIO_RETENTION_DAYS sh "$store" init 2>&1)" && fail "init ran with no settings"
out="$(NOEMA_AUDIO_STORE_DIR="$good" sh "$store" init 2>&1)" && fail "init ran with no retention (the number is a plan value, never a default)"
for bad in "" 0 -1 1d 1.5 007 " 7" 99999; do
  out="$(NOEMA_AUDIO_STORE_DIR="$good" NOEMA_AUDIO_RETENTION_DAYS="$bad" sh "$store" init 2>&1)" && fail "init ran with retention '$bad'"
  [ ! -e "$good" ] || fail "a store was made with retention '$bad'"
done
# The path: absolute, plain, not inside the repository, not a link.
for bad in "" "relative/store" "$work/../x" "$work/a/../b" "$work/a b"; do
  out="$(env_run "$bad" 30 init)" && fail "init ran with the path '$bad'"
done
repo="$(cd "$here/../../.." && pwd)"
out="$(env_run "$repo/audio-store-test" 30 init)" && fail "init ran with a path inside the repository"
[ ! -e "$repo/audio-store-test" ] || { rmdir "$repo/audio-store-test"; fail "a store was made inside the repository"; }
mkdir "$work/real"; ln -s "$work/real" "$work/link"
out="$(env_run "$work/link" 30 init)" && fail "init ran on a symbolic link"

# init makes the store at mode 0700, owned by the caller, and says so without a path of a recording.
out="$(env_run "$good" 30 init)" || fail "init failed: $out"
[ "$(stat -c %a "$good")" = "700" ] || fail "the store is not mode 700"
[ "$(stat -c %u "$good")" = "$(id -u)" ] || fail "the store is not owned by the caller"
env_run "$good" 30 check >/dev/null || fail "check refused a good store"
# A second init keeps a good store; a store that is looser is refused, never silently changed.
env_run "$good" 30 init >/dev/null || fail "a second init of a good store failed"
mkdir "$work/loose"; chmod 755 "$work/loose"
out="$(env_run "$work/loose" 30 init)" && fail "init accepted an existing store with mode 755"
[ "$(stat -c %a "$work/loose")" = "755" ] || fail "init changed the mode of a store it refused"
chmod 770 "$good"
out="$(env_run "$good" 30 check)" && fail "check accepted mode 770"
out="$(env_run "$good" 30 purge)" && fail "purge ran on a store that is not mode 700"
chmod 700 "$good"

# purge removes the files older than the retention, keeps the newer, prints a count and no name, and never follows a link out of the store.
mkdir -p "$good/session-a"
printf synthetic > "$good/old-one.wav"; printf synthetic > "$good/session-a/old-two.wav"; printf synthetic > "$good/new-one.wav"
touch -d '40 days ago' "$good/old-one.wav" "$good/session-a/old-two.wav"
touch -d '5 days ago' "$good/new-one.wav"
printf synthetic > "$work/outside.wav"; touch -d '90 days ago' "$work/outside.wav"
ln -s "$work/outside.wav" "$good/linked.wav"
out="$(env_run "$good" 30 purge)" || fail "purge failed: $out"
case "$out" in *old-one*|*old-two*|*session-a*|*wav*) fail "purge printed a file name: $out" ;; esac
echo "$out" | grep -Eq 'purged 2 files' || fail "purge did not report 2 files: $out"
[ ! -e "$good/old-one.wav" ] && [ ! -e "$good/session-a/old-two.wav" ] || fail "an old file was kept"
[ -e "$good/new-one.wav" ] || fail "a recent file was removed"
[ -e "$work/outside.wav" ] || fail "purge followed a link out of the store"
[ ! -d "$good/session-a" ] || fail "an emptied folder was kept"
echo "audio-store test: ok"
