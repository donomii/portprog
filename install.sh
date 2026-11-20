#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
destination="$project_dir/dist/portprog"

"$project_dir/build.sh"
mkdir -p "$destination"
mkdir -p "$destination/catalog" "$destination/profiles"
cp "$project_dir/portprog" "$destination/portprog"
cp "$project_dir/SPEC.md" "$destination/SPEC.md"
cp "$project_dir/MakeYourOwnPackage.md" "$destination/MakeYourOwnPackage.md"
cp "$project_dir/LICENSE" "$destination/LICENSE"
cp "$project_dir"/catalog/*.json "$destination/catalog/"
cp "$project_dir"/profiles/*.json "$destination/profiles/"
cp "$project_dir/packaging/run.sh" "$destination/run.sh"
chmod +x "$destination/portprog" "$destination/run.sh"

if [ -f "$destination/portprog.conf" ]; then
    printf 'Kept existing editable configuration: %s/portprog.conf\n' "$destination"
else
    cp "$project_dir/portprog.conf" "$destination/portprog.conf"
    printf 'Installed editable default configuration: %s/portprog.conf\n' "$destination"
fi

printf 'Portable distribution ready: %s\n' "$destination"
printf 'Start it with: %s/run.sh\n' "$destination"
