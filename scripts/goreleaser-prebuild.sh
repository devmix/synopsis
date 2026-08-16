#!/usr/bin/env bash
# Pre-build hook for .goreleaser.yaml (runs as `before.hooks`).
# Creates fixed-path inputs so the build env values can stay literal:
#   /tmp/synopsis-goreleaser/sqlite-include/sqlite3.h -> go-sqlite3 binding header
#     (sqlite-vec-go-bindings does #include "sqlite3.h")
#   /tmp/synopsis-goreleaser/darwin-stubs -> scripts/darwin-stubs
#   /tmp/synopsis-goreleaser/zig-sdk      -> $(zig env) .lib_dir/libc/darwin
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAGE="/tmp/synopsis-goreleaser"

rm -rf "$STAGE"
mkdir -p "$STAGE/sqlite-include"

SQLITE3_DIR="$(go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3)"
ln -sf "${SQLITE3_DIR}/sqlite3-binding.h" "$STAGE/sqlite-include/sqlite3.h"

ln -sfn "$ROOT/scripts/darwin-stubs" "$STAGE/darwin-stubs"

ZIG_LIB_DIR="$(zig env | sed -n 's/.*\.lib_dir = "\([^"]*\)".*/\1/p')"
if [ ! -d "$ZIG_LIB_DIR/libc/darwin" ]; then
  echo "zig darwin SDK not found at $ZIG_LIB_DIR/libc/darwin" >&2
  exit 1
fi
ln -sfn "$ZIG_LIB_DIR/libc/darwin" "$STAGE/zig-sdk"

echo "goreleaser prebuild stage ready: $STAGE"
