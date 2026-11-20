#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
"$project_dir/build.sh"
exec "$project_dir/portprog"
