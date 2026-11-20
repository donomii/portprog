#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
"$project_dir/build.sh"
"$project_dir/portprog" plan
