#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$project_dir"

go test ./...
go vet ./...
go build -o portprog .
"$project_dir/portprog" -test
