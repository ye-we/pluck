#!/bin/sh
# Builds the pluck programs the plugin ships, one per platform, into dist/.
set -e
cd "$(dirname "$0")/.."
go vet ./...
go test ./...
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
	os=${target%/*}
	arch=${target#*/}
	out="dist/pluck-$os-$arch"
	[ "$os" = windows ] && out="$out.exe"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$out" .
	echo "built $out"
done
