#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$project_dir"

go build -o portprog .
printf 'Built Portprog: %s/portprog\n' "$project_dir"
