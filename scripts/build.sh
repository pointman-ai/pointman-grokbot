#!/bin/sh
# Builds grokbot-wake for every platform a node installs it on, as the release's assets:
# dist/grokbot-wake-<platform>.tar.gz, each holding one file named grokbot-wake-<platform>
# (the node unpacks the file named as its asset, less .tar.gz). Usage: scripts/build.sh <version>
set -eu
version=${1:?usage: scripts/build.sh <version>}
cd "$(dirname "$0")/.."
rm -rf dist && mkdir -p dist
for target in darwin-arm64:darwin:arm64 darwin-x64:darwin:amd64 linux-arm64:linux:arm64 linux-x64:linux:amd64; do
  platform=${target%%:*}; rest=${target#*:}; goos=${rest%%:*}; goarch=${rest#*:}
  name="grokbot-wake-$platform"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$version" -o "dist/$name" .
  tar -C dist -czf "dist/$name.tar.gz" "$name"
  rm "dist/$name"
done
ls -l dist
