#!/bin/sh
# The private raw-audio store of the bench host (A451, A377 to A379; REQ-VOI-004, AGENT-13, AGENT-16). A directory on the bench host, mode 0700, owned by the account that runs the voice recordings,
# with a retention purge on a timer. It is never uploaded, never in the repository, never in a CI artifact and never mounted into a container this repository starts.
#
#   store.sh init                    make the store, or accept an existing one only if it is already right
#   store.sh check                   say whether the store is right (exit 0) or why not (exit 1)
#   store.sh purge                   delete the files older than the retention, then the folders that are empty
#   store.sh remove <speaker_ref>    delete one speaker's whole folder (a withdrawn recording: within 7 days, VOI-05)
#   store.sh scan                    count the objects and the objects that are not under a speaker_ref (the plan's "no object without a speaker_ref"); exit 1 if any
#
# Layout (VOI-05, noema#201): <store>/<speaker_ref>/...; a speaker_ref is a plain name of letters, digits, dot, dash and underscore. The retention runs from the ANALYSIS of a recording, not from its
# creation: the voice runner sets each FILE's modification time to the analysis date when the analysis of its session is finished (Voice's read of noema#819), and purge removes a file whose own
# modification time is older than the retention. Synthetic fixtures live in the same store, labelled SYNTHETIC, and follow the same purge. Disk encryption at rest is not done here: it is the host's,
# a Founder's step when the volume is provisioned (this script cannot see it).
#
# Both settings are required and have no default: NOEMA_AUDIO_STORE_DIR (an absolute path of letters, digits, dot, dash, underscore and slash, with no `..`, not a link, not inside this
# repository) and NOEMA_AUDIO_RETENTION_DAYS (a whole number of days, 1 to 9999, no leading zero; the number is the plan's, VOI-05, and is set on the host, never here). The store must be a directory
# owned by the caller with mode exactly 700; init never loosens or tightens an existing store, it refuses one that is not right. Output names counts and reasons only, never a file name or a path
# inside the store: a file name can hold a speaker. Exit 0 ok, 1 refused, 2 bad usage. SYNTHETIC tests only (test_store.sh).
set -u
fail() { echo "audio-store: $1" >&2; exit 1; }
usage="usage: store.sh init|check|purge|scan, or store.sh remove <speaker_ref>"
[ $# -ge 1 ] || { echo "$usage" >&2; exit 2; }
cmd="$1"
case "$cmd" in
  init|check|purge|scan) [ $# -eq 1 ] || { echo "$usage" >&2; exit 2; } ;;
  remove) [ $# -eq 2 ] || { echo "$usage" >&2; exit 2; } ;;
  *) echo "$usage" >&2; exit 2 ;;
esac

stat -c %a / > /dev/null 2>&1 || fail "GNU stat is required (stat -c); this host has another stat"
dir="${NOEMA_AUDIO_STORE_DIR-}"
days="${NOEMA_AUDIO_RETENTION_DAYS-}"
[ -n "$dir" ] || fail "NOEMA_AUDIO_STORE_DIR is not set (no default)"
[ -n "$days" ] || fail "NOEMA_AUDIO_RETENTION_DAYS is not set (no default: the number is the plan's)"
case "$days" in
  [1-9]|[1-9][0-9]|[1-9][0-9][0-9]|[1-9][0-9][0-9][0-9]) ;;
  *) fail "NOEMA_AUDIO_RETENTION_DAYS must be a whole number of days, 1 to 9999, with no leading zero" ;;
esac
case "$dir" in
  /*) ;;
  *) fail "the store path must be absolute" ;;
esac
case "$dir" in
  *[!A-Za-z0-9._/-]*) fail "the store path may hold only letters, digits, dot, dash, underscore and slash" ;;
esac
case "$dir/" in
  */../*|*/./*) fail "the store path must be a plain path (no .., no .)" ;;
esac
case "$dir" in
  *//*) fail "the store path must be a plain path (no empty part)" ;;
esac
repo="$(cd "$(dirname "$0")/../../.." && pwd -P)"
case "$dir/" in
  "$repo"/*) fail "the store must not be inside this repository" ;;
esac
[ ! -L "$dir" ] || fail "the store must not be a symbolic link"

verify() {
  [ -d "$dir" ] || fail "the store does not exist"
  [ ! -L "$dir" ] || fail "the store must not be a symbolic link"
  real="$(cd "$dir" && pwd -P)"
  case "$real/" in
    "$repo"/*) fail "the store resolves to a place inside this repository" ;;
  esac
  [ "$(stat -c %u "$dir")" = "$(id -u)" ] || fail "the store is not owned by the account that runs this"
  [ "$(stat -c %a "$dir")" = "700" ] || fail "the store is not mode 700"
}

case "$cmd" in
  init)
    if [ -e "$dir" ]; then
      verify
      echo "audio-store: the store exists and is right (mode 700, yours)"
    else
      parent="$(dirname "$dir")"
      [ -d "$parent" ] || fail "the parent of the store does not exist"
      ( umask 077 && mkdir -m 700 "$dir" ) || fail "the store could not be made"
      verify
      echo "audio-store: the store was made (mode 700, yours)"
    fi
    ;;
  check)
    verify
    echo "audio-store: the store is right (mode 700, yours)"
    ;;
  purge)
    verify
    # Files only (a link is not a file here and is never followed), older than the retention by each file's own modification time. The count is printed, never a name.
    n="$(find "$dir" -mindepth 1 -type f -mtime "+$((days - 1))" -print | wc -l | tr -d ' ')"
    find "$dir" -mindepth 1 -type f -mtime "+$((days - 1))" -delete
    find "$dir" -mindepth 1 -type d -empty -delete
    echo "audio-store: purged $n files older than $days days"
    ;;
  remove)
    verify
    ref="$2"
    case "$ref" in
      ''|.|..|*[!A-Za-z0-9._-]*) fail "the speaker reference is not a plain name" ;;
    esac
    [ -d "$dir/$ref" ] && [ ! -L "$dir/$ref" ] || fail "no such speaker folder"
    n="$(find "$dir/$ref" -type f | wc -l | tr -d ' ')"
    rm -rf "$dir/$ref"
    echo "audio-store: removed $n files of one speaker"
    ;;
  scan)
    verify
    # Everything at the top of the store must be a folder with a plain speaker_ref name. A loose file, a link, or a folder with another name (and each file in it) is an object without a speaker_ref.
    # A link anywhere is refused too. Counts only: no name, no content.
    total="$(find "$dir" -mindepth 1 \( -type f -o -type l \) | wc -l | tr -d ' ')"
    bad=0
    for entry in "$dir"/* "$dir"/.[!.]*; do
      [ -e "$entry" ] || [ -L "$entry" ] || continue
      base="$(basename "$entry")"
      if [ -L "$entry" ] || [ ! -d "$entry" ]; then bad=$((bad + 1)); continue; fi
      case "$base" in
        *[!A-Za-z0-9._-]*|.*) inside="$(find "$entry" -type f | wc -l | tr -d ' ')"; [ "$inside" -ge 1 ] || inside=1; bad=$((bad + inside)) ;;
      esac
    done
    links="$(find "$dir" -mindepth 2 -type l | wc -l | tr -d ' ')"
    # A file or a folder open to group or other (any of the 077 bits) is counted too: the store is 0700, so this is a second line of defence for a store that is copied or restored.
    open="$(find "$dir" -mindepth 1 \( -type f -o -type d \) -perm /077 | wc -l | tr -d ' ')"
    echo "audio-store: $total objects; $bad objects without a speaker_ref; $links links inside speaker folders; $open objects open to group or other"
    [ "$bad" -eq 0 ] && [ "$links" -eq 0 ] && [ "$open" -eq 0 ] || exit 1
    ;;
esac
