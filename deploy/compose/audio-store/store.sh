#!/bin/sh
# The private raw-audio store of the bench host (A451, A377 to A379; REQ-VOI-004, AGENT-13, AGENT-16). A directory on the bench host, mode 0700, owned by the account that runs the voice recordings,
# with a retention purge on a timer. It is never uploaded, never in the repository, never in a CI artifact and never mounted into a container this repository starts.
#
#   store.sh init     make the store, or accept an existing one only if it is already right
#   store.sh check    say whether the store is right (exit 0) or why not (exit 1)
#   store.sh purge    delete the files older than the retention, then the folders that are empty
#
# Both settings are required and have no default: NOEMA_AUDIO_STORE_DIR (an absolute path of letters, digits, dot, dash, underscore and slash, with no `..`, not a link, not inside this
# repository) and NOEMA_AUDIO_RETENTION_DAYS (a whole number of days, 1 to 9999, no leading zero; the number is the plan's, VOI-05, and is set on the host, never here). The store must be a directory
# owned by the caller with mode exactly 700; init never loosens or tightens an existing store, it refuses one that is not right. Output names counts and reasons only, never a file name or a path
# inside the store: a file name can hold a speaker. Exit 0 ok, 1 refused, 2 bad usage. SYNTHETIC tests only (test_store.sh).
set -u
fail() { echo "audio-store: $1" >&2; exit 1; }
[ $# -eq 1 ] || { echo "usage: store.sh init|check|purge" >&2; exit 2; }
cmd="$1"
case "$cmd" in init|check|purge) ;; *) echo "usage: store.sh init|check|purge" >&2; exit 2 ;; esac

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
    # Files only (a link is not a file here and is never followed), older than the retention by modification time. The count is printed, never a name.
    n="$(find "$dir" -mindepth 1 -type f -mtime "+$((days - 1))" -print | wc -l | tr -d ' ')"
    find "$dir" -mindepth 1 -type f -mtime "+$((days - 1))" -delete
    find "$dir" -mindepth 1 -type d -empty -delete
    echo "audio-store: purged $n files older than $days days"
    ;;
esac
