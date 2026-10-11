#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -L)
cd "$root"
go test ./...
"$root/build.sh"
"$root/portprog" -test
"$root/portprog" --validate
