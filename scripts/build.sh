#!/bin/sh
# Builds the pluck programs the plugin ships, one per platform, into dist/.
# The same source always builds the same bytes: Go is pinned in go.mod, and
# nothing about this machine or the git state goes into the programs.
set -e
cd "$(dirname "$0")/.."
GOTOOLCHAIN=$(sed -n 's/^toolchain //p' go.mod)
export GOTOOLCHAIN
go vet ./...
go test ./...
# go build leaves a program in place if the build ID inside it matches, even
# when the rest of the file was changed, so CI couldn't spot a tampered one.
rm -f dist/pluck-*
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
	os=${target%/*}
	arch=${target#*/}
	out="dist/pluck-$os-$arch"
	[ "$os" = windows ] && out="$out.exe"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$out" .
	echo "built $out"
done
