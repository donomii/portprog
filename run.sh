#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -L)
if [ -f "$root/updater.go" ]; then
  "$root/build.sh"
else
  if [ ! -x "$root/portprog" ]; then
    printf 'Portprog launcher expected executable %s/portprog or buildable source %s/updater.go; neither is available.\n' "$root" "$root" >&2
    exit 1
  fi
fi
"$root/portprog" "$@"
