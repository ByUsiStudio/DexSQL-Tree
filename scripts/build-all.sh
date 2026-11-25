#!/usr/bin/env bash
# Cross-compile dexsql-tree for every supported platform.
#
# The Go engine has no cgo dependencies (modernc.org/sqlite is pure Go), so a
# single host can produce all binaries, including the ARM64 ones.
#
# Usage:
#   scripts/build-all.sh                 build into dist/dexsql-tree-<version>
#   VERSION=0.4.0 scripts/build-all.sh   override the version stamped into the binary
#   OUT=/tmp/out scripts/build-all.sh    override the output directory
set -euo pipefail

VERSION="${VERSION:-1.0.0}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${OUT:-$ROOT/dist/dexsql-tree-$VERSION}"
LDFLAGS="-s -w -X github.com/byusi-studio/dexsql-tree/internal/api.Version=$VERSION"

# goos|goarch|goarm|exe-suffix
TARGETS=(
  "linux|amd64||"
  "linux|arm64||"
  "linux|arm|7|"
  "darwin|amd64||"
  "darwin|arm64||"
  "windows|amd64||.exe"
  "windows|arm64||.exe"
  "freebsd|amd64||"
  "freebsd|arm64||"
)

cd "$ROOT"
rm -rf "$OUT"
mkdir -p "$OUT"

built=()
for entry in "${TARGETS[@]}"; do
  IFS='|' read -r goos goarch goarm ext <<< "$entry"
  dir="$OUT/$goos-$goarch"
  if [ -n "$goarm" ]; then
    dir="$OUT/$goos-armv$goarm"
  fi
  name="dexsql-tree$ext"
  printf '==> %s/%s%s\n' "$goos" "$goarch" "${goarm:+ (GOARM=$goarm)}"
  mkdir -p "$dir"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$dir/$name" ./cmd/dexsql-tree
  built+=("$goos-$goarch${goarm:+-armv$goarm}/$name")
done

# Sample input so the archive can be smoke-tested without an APK at hand.
go run ./cmd/dexgen -out "$OUT/demo" >/dev/null
echo "demo: $(ls "$OUT/demo" | tr '\n' ' ')"

# The Java artefacts used to be copied by hand after every release, which is
# exactly the step that gets forgotten. Build them here or say why not.
if command -v mvn >/dev/null 2>&1; then
  echo "==> java client and GUI"
  ( cd "$ROOT/java" && mvn -B -q -DskipTests package )
  mkdir -p "$OUT/java"
  cp "$ROOT/java/dexsql-tree-client/target/dexsql-tree-client-$VERSION.jar" "$OUT/java/"
  cp "$ROOT/java/dexsql-tree-gui/target/dexsql-tree-gui-$VERSION.jar" "$OUT/java/"
else
  echo "==> mvn not found: skipping the Java client and GUI (install Maven to include them)"
fi

cp docs/release-notes-"$VERSION".md "$OUT/RELEASE-NOTES.md" 2>/dev/null || true
cat > "$OUT/BUILD.md" <<EOF
# dexsql-tree $VERSION binaries

Extract the directory matching your platform and run the binary. No runtime
dependency is needed: the engine is a single static executable (no cgo, no
SQLite shared library).

| Directory | Platform | Notes |
| --- | --- | --- |
| linux-amd64 | Linux x86-64 | CI, servers |
| linux-arm64 | Linux ARM64 | Raspberry Pi 4/5, ARM servers, Asahi |
| linux-armv7 | Linux ARMv7 | Raspberry Pi 2/3, 32-bit ARM boards |
| darwin-amd64 | macOS Intel | |
| darwin-arm64 | macOS Apple Silicon | M1/M2/M3 |
| windows-amd64 | Windows x64 | |
| windows-arm64 | Windows on ARM | Surface Pro X, Snapdragon X |
| freebsd-amd64 | FreeBSD x86-64 | |
| freebsd-arm64 | FreeBSD ARM64 | |

Quick check:

\`\`\`bash
./linux-arm64/dexsql-tree version
./linux-arm64/dexsql-tree query --input demo/demo.apk \\
  --sql "SELECT descriptor FROM methods"
./linux-arm64/dexsql-tree stats --input demo/demo.apk
\`\`\`

\`java/\` holds the client and JavaFX GUI jars (thin jars; Jackson is the only
runtime dependency of the client).

Verify the downloads with:

\`\`\`bash
sha256sum -c sha256sums.txt
\`\`\`

Chinese prompts: set \`DEXSQL_TREE_ZH=true\`, or run
\`dexsql-tree lang set zh --write\` once to persist it in your shell profile.
EOF

( cd "$OUT" && find . -type f \( -name 'dexsql-tree*' -o -name 'demo.apk' -o -name 'demo.dex' -o -name '*.jar' \) -print0 \
    | sort -z | xargs -0 sha256sum > sha256sums.txt )

echo
echo "built ${#built[@]} targets into $OUT"
