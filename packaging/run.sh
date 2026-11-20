#!/bin/sh
set -eu

install_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$install_dir"
exec "$install_dir/portprog"
