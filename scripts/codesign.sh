#!/usr/bin/env bash
# Signs the binary $1 with a stable identity when ONETT_CODESIGN_IDENTITY is set (see README.md,
# "Run it as a service"). macOS keys privacy grants such as Full Disk Access to the code
# signature. The default ad-hoc signature is the binary's hash, so every build is a new app and
# loses its grants; a certificate gives every build the same identity.
set -euo pipefail

out=${1:?usage: codesign.sh BINARY}
if [ -n "${ONETT_CODESIGN_IDENTITY:-}" ]; then
  codesign --force --sign "$ONETT_CODESIGN_IDENTITY" --identifier io.onett.onett "$out"
elif [ "$(uname -s)" = Darwin ]; then
  echo "warning: ONETT_CODESIGN_IDENTITY is not set; macOS privacy grants (Full Disk Access) will not survive updates" >&2
fi
