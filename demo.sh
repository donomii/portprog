#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -L)
"$root/build.sh"
"$root/portprog" --validate
"$root/portprog" -test
