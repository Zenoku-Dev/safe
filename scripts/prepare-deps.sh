#!/bin/bash
# Готовит ./deps из Debian-пакетов Go-библиотек uTLS (используется через go.mod replace).
# Нужны только на машине сборки. Требует: apt, ar, tar, xz. Arch-независимо (Go).
set -euo pipefail
TMP=${TMPDIR:-/tmp}/safe-deps-$$
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP" "$(pwd)/deps"
debs="golang-refraction-networking-utls-dev
golang-github-andybalholm-brotli-dev
golang-github-cespare-xxhash-dev
golang-github-klauspost-compress-dev
golang-github-klauspost-cpuid-dev
golang-golang-x-crypto-dev
golang-golang-x-net-dev
golang-golang-x-sys-dev
golang-golang-x-text-dev"
for p in $debs; do (cd "$TMP" && apt-get download "$p"); done
for d in "$TMP"/*.deb; do ar p "$d" data.tar.xz | tar xJ -C "$TMP"; done
S="$TMP/usr/share/gocode/src"
cp -r "$S/github.com/refraction-networking/utls" deps/utls
cp -r "$S/github.com/andybalholm/brotli" deps/brotli
cp -r "$S/github.com/klauspost/compress" deps/compress
cp -r "$S/github.com/klauspost/cpuid" deps/cpuid
# cespare/xxhash в Debian — v2 (API New/Digest/Sum64); модуль в дереве лежит по v1-пути github.com/cespare/xxhash
mkdir -p deps/xxhash && cp -r "$S/github.com/cespare/xxhash"/* deps/xxhash/ && printf 'module github.com/cespare/xxhash\n\ngo 1.11\n' > deps/xxhash/go.mod
for p in crypto net sys text; do cp -r "$S/golang.org/x/$p" deps/x-$p; done
# klauspost/compress (zstd) импортирует github.com/cespare/xxhash — v1-путь нужен с modern API (v2)
sed -i 's#"github.com/OneOfOne/xxhash"#"github.com/cespare/xxhash"#' deps/compress/zstd/*.go 2>/dev/null || true
echo "deps/ готов ($(du -sh deps | cut -f1))"
