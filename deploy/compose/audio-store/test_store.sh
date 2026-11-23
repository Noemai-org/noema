#!/bin/sh
# Tests for store.sh, the private raw-audio store of the bench host (A451, A377 to A379; REQ-VOI-004). Real directories and real modes, run as the runner's own user. SYNTHETIC: the files hold
# the word "synthetic", never audio. Written before store.sh exists (the first run fails because there is no store.sh).
set -u
umask 077  # every file and folder the test makes is 0600 and 0700, as the voice runner's are (scan refuses anything open to group or other)
here="$(cd "$(dirname "$0")" && pwd)"
store="$here/store.sh"
fail() { echo "audio-store test: $1"; exit 1; }
[ -f "$store" ] || fail "store.sh does not exist"
env -u NOEMA_AUDIO_STORE_DIR sh "$store" init 2>&1 | grep -q "GNU" && fail "store.sh refused a GNU stat"
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

# The layout (VOI-05, noema#201): <store>/<speaker_ref>/..., and the retention runs from the ANALYSIS of a recording, not from its creation: the voice runner sets each FILE's modification time to the
# analysis date, and purge removes a file whose own modification time is older than the retention (a folder's own time is never read).
mkdir -p "$good/spk_aa/s1" "$good/spk_aa/s2" "$good/spk_bb/s3"
printf synthetic > "$good/spk_aa/s1/a.wav"; printf synthetic > "$good/spk_aa/s2/b.wav"; printf synthetic > "$good/spk_bb/s3/c.wav"; printf synthetic > "$good/spk_aa/s1/d.wav"
touch -d '5 days ago' "$good/spk_aa/s1/a.wav" "$good/spk_aa/s1/d.wav"  # analysed 5 days ago: kept
touch -d '40 days ago' "$good/spk_aa/s2/b.wav" "$good/spk_bb/s3/c.wav" # analysed 40 days ago: removed
touch -d '40 days ago' "$good/spk_aa/s1"                               # a folder's own old time does not remove a file that is recent
printf synthetic > "$work/outside.wav"; ln -s "$work/outside.wav" "$good/spk_aa/linked.wav"
out="$(env_run "$good" 30 purge)" || fail "purge failed: $out"
case "$out" in *a.wav*|*b.wav*|*spk_*|*s1*|*s2*|*s3*) fail "purge printed a name: $out" ;; esac
echo "$out" | grep -Eq 'purged 2 files' || fail "purge did not report 2 files: $out"
[ -e "$good/spk_aa/s1/a.wav" ] && [ -e "$good/spk_aa/s1/d.wav" ] || fail "a file analysed 5 days ago was removed"
[ ! -e "$good/spk_aa/s2/b.wav" ] && [ ! -e "$good/spk_bb/s3/c.wav" ] || fail "a file analysed 40 days ago was kept"
[ ! -d "$good/spk_bb" ] && [ ! -d "$good/spk_aa/s2" ] || fail "an emptied folder was kept"
[ -e "$work/outside.wav" ] || fail "purge followed a link out of the store"
rm -f "$good/spk_aa/linked.wav"

# remove <speaker_ref>: a withdrawn speaker's whole folder, by reference; refuses a reference that is not plain; prints a count, never a name. SR on noema#819: a removal leaves a trace, one
# count-only line (date and count, never the reference) in a log that only this account can read, so the 7-day promise can be shown. The log is required for `remove` and a removal that cannot be
# logged is refused with nothing deleted.
rlog="$work/removals.log"
remove_with() { # reference [log setting]; the log setting defaults to $rlog, "-" means unset
  if [ "${2-}" = "-" ]; then
    env -u NOEMA_AUDIO_REMOVAL_LOG NOEMA_AUDIO_STORE_DIR="$good" NOEMA_AUDIO_RETENTION_DAYS=30 sh "$store" remove "$1" 2>&1
  else
    NOEMA_AUDIO_STORE_DIR="$good" NOEMA_AUDIO_RETENTION_DAYS=30 NOEMA_AUDIO_REMOVAL_LOG="${2-$rlog}" sh "$store" remove "$1" 2>&1
  fi
}
mkdir -p "$good/spk_cc/s9" "$good/spk_dd/s9"; printf synthetic > "$good/spk_cc/s9/x.wav"; printf synthetic > "$good/spk_dd/s9/y.wav"
for bad in "" "../spk_dd" "spk_dd/s9" "spk dd" "." ".." "/etc" "spk_dd/"; do
  out="$(remove_with "$bad")" && fail "remove ran with the reference '$bad'"
done
[ -e "$good/spk_dd/s9/y.wav" ] || fail "a refused remove deleted something"
out="$(remove_with spk_dd -)" && fail "remove ran with no removal log setting"
[ -e "$good/spk_dd/s9/y.wav" ] || fail "a removal with no log deleted something"
[ ! -e "$rlog" ] || fail "a log was made by a refused removal"
out="$(remove_with spk_cc)" || fail "remove failed: $out"
case "$out" in *spk_cc*|*x.wav*) fail "remove printed a name: $out" ;; esac
echo "$out" | grep -Eq 'removed 1 files' || fail "remove did not report 1 file: $out"
[ ! -e "$good/spk_cc" ] || fail "the speaker folder was kept"
[ -e "$good/spk_dd/s9/y.wav" ] || fail "another speaker's folder was removed"
out="$(remove_with spk_zz)" && fail "remove of an absent speaker reported success"
ln -s "$work/real" "$good/spk_link"
out="$(remove_with spk_link)" && fail "remove followed a link"
rm "$good/spk_link"
# the trace: one line per removal, date and count only, in a file at mode 0600, appended to (never rewritten)
[ -f "$rlog" ] && [ ! -L "$rlog" ] || fail "the removal log was not made"
[ "$(stat -c %a "$rlog")" = "600" ] || fail "the removal log is not mode 600"
[ "$(wc -l < "$rlog" | tr -d ' ')" = "1" ] || fail "the removal log does not hold one line after one removal"
grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z removal 1 files$' "$rlog" || fail "the removal log line is not 'date removal N files'"
case "$(cat "$rlog")" in *spk_*|*cc*|*x.wav*) fail "the removal log names a speaker or a file" ;; esac
mkdir -p "$good/spk_ee/s1"; printf synthetic > "$good/spk_ee/s1/a.wav"; printf synthetic > "$good/spk_ee/s1/b.wav"
out="$(remove_with spk_ee)" || fail "a second removal failed: $out"
[ "$(wc -l < "$rlog" | tr -d ' ')" = "2" ] || fail "the second removal did not append a line"
grep -Eq ' removal 2 files$' "$rlog" || fail "the second line does not say 2 files"
# a log that cannot be trusted refuses the removal with nothing deleted: a relative path, a hostile character, inside the store, inside the repository, a link, a loose mode, no parent
mkdir -p "$good/spk_ff/s1"; printf synthetic > "$good/spk_ff/s1/a.wav"
printf '' > "$work/loose.log"; chmod 644 "$work/loose.log"
ln -s "$rlog" "$work/linked.log"
for badlog in "relative.log" "$work/a b.log" "$work/x;y.log" "$good/inside.log" "$repo/removal-test.log" "$work/linked.log" "$work/loose.log" "$work/nodir/removals.log" "$work/../removals.log"; do
  out="$(remove_with spk_ff "$badlog")" && fail "remove ran with the log '$badlog'"
  [ -e "$good/spk_ff/s1/a.wav" ] || fail "a removal with the bad log '$badlog' deleted something"
done
[ ! -e "$repo/removal-test.log" ] || { rm -f "$repo/removal-test.log"; fail "a log was made inside the repository"; }
# Codex P2 on noema#871: the checks above are on TEXT; a log whose FOLDER is a link into the speaker's folder or into the store would be created there and deleted with the recordings, so the
# log's folder is resolved first. A store path that ends in a slash must not hide a log directly inside the store either.
ln -s "$good/spk_ff" "$work/alias_speaker"; ln -s "$good" "$work/alias_store"
for badlog in "$work/alias_speaker/removals.log" "$work/alias_store/removals.log" "$work/alias_store/spk_ff/removals.log"; do
  out="$(remove_with spk_ff "$badlog")" && fail "remove ran with the aliased log '$badlog'"
  [ -e "$good/spk_ff/s1/a.wav" ] || fail "a removal with the aliased log '$badlog' deleted something"
  [ ! -e "$good/spk_ff/removals.log" ] && [ ! -e "$good/removals.log" ] || fail "a log was made inside the store through '$badlog'"
done
out="$(NOEMA_AUDIO_STORE_DIR="$good/" NOEMA_AUDIO_RETENTION_DAYS=30 NOEMA_AUDIO_REMOVAL_LOG="$good/inside2.log" sh "$store" remove spk_ff 2>&1)" && fail "remove ran with a store path that ends in a slash and a log inside the store"
[ -e "$good/spk_ff/s1/a.wav" ] && [ ! -e "$good/inside2.log" ] || fail "the trailing-slash store hid a log inside the store"
out="$(NOEMA_AUDIO_STORE_DIR="$good/" NOEMA_AUDIO_RETENTION_DAYS=30 NOEMA_AUDIO_REMOVAL_LOG="$work/slash.log" sh "$store" remove spk_ff 2>&1)" || fail "a store path that ends in a slash was refused for a good log: $out"
[ ! -e "$good/spk_ff" ] || fail "the trailing-slash store removal did not remove the folder"

# scan: the plan's "no object without a speaker_ref": counts only, exit 1 if any object is outside <speaker_ref>/, never a name or a content.
out="$(env_run "$good" 30 scan)" || fail "scan failed on a store with every object under a speaker: $out"
echo "$out" | grep -Eq ' [1-9][0-9]* objects; 0 objects without a speaker_ref' || fail "scan did not report the objects and 0 without a speaker_ref: $out"
# An object open to group or other is refused too (SR on noema#819): the store is 0700, so a loose file mode is a second line of defence for a store that is ever copied or restored.
printf synthetic > "$good/spk_dd/open.wav"; chmod 644 "$good/spk_dd/open.wav"
out="$(env_run "$good" 30 scan)" && fail "scan passed with a file open to group and other"
echo "$out" | grep -Eq ' 1 objects open to group or other' || fail "scan did not count the open file: $out"
case "$out" in *open.wav*|*spk_dd*) fail "scan printed a name: $out" ;; esac
chmod 600 "$good/spk_dd/open.wav"
out="$(env_run "$good" 30 scan)" || fail "scan failed once the file was 0600: $out"
printf synthetic > "$good/loose.wav"; mkdir "$good/bad.ref!"; ln -s "$work/real" "$good/spk_link2"
out="$(env_run "$good" 30 scan)" && fail "scan passed with loose objects"
case "$out" in *loose*|*bad*|*spk_*|*synthetic*) fail "scan printed a name or a content: $out" ;; esac
echo "$out" | grep -Eq ' [1-9][0-9]* objects without a speaker_ref' || fail "scan did not count them: $out"
echo "audio-store test: ok"
