#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$ROOT"

VERSION="$(date +%y%m%d%H%S)"
LDFLAGS="-s -w -X main.Version=${VERSION}"

if [ "${SKIP_FRONTEND:-}" != "1" ]; then
    (cd web && npm run build)
fi

mkdir -p dist .gocache .gomodcache

CGO_ENABLED=0 GOCACHE="$ROOT/.gocache" GOMODCACHE="$ROOT/.gomodcache" GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "$LDFLAGS" -o dist/pdai-linux-amd64 .

CGO_ENABLED=0 GOCACHE="$ROOT/.gocache" GOMODCACHE="$ROOT/.gomodcache" GOOS=linux GOARCH=arm64 \
    go build -trimpath -ldflags "$LDFLAGS" -o dist/pdai-linux-arm64 .

printf 'Built Pdai version %s\n' "$VERSION"
printf '  dist/pdai-linux-amd64\n'
printf '  dist/pdai-linux-arm64\n'
