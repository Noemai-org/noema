#!/usr/bin/env bash
# Installs gitleaks and syft from pinned releases, verifying SHA-256 (PLT-02). Usage: install_tools.sh <dir>
set -euo pipefail
dest="${1:?destination directory}"
mkdir -p "$dest"
tmp="$(mktemp -d)"
fetch() { # name url sha256 member
  curl -sSL --fail -o "$tmp/$1.tgz" "$2"
  echo "$3  $tmp/$1.tgz" | sha256sum -c -
  tar -xzf "$tmp/$1.tgz" -C "$dest" "$4"
}
fetch gitleaks https://github.com/gitleaks/gitleaks/releases/download/v8.29.1/gitleaks_8.29.1_linux_x64.tar.gz \
  e4eb209d04e20339d77122a3bdf9cd41351255cfb27ebcb75e85325e04f88924 gitleaks
fetch syft https://github.com/anchore/syft/releases/download/v1.9.0/syft_1.9.0_linux_amd64.tar.gz \
  4c7db3f86a7cd86997fc37da412ac4c6031bb8d0a319a0b152791e53cd240ac4 syft
