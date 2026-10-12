#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -L)
cd "$root"
go build -ldflags=-B=gobuildid -o "$root/portprog" .
