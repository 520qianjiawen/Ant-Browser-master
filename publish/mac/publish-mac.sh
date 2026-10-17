#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUTPUT_DIR="$ROOT_DIR/publish/output"
STAGING_ROOT="$ROOT_DIR/publish/staging/mac"
ARCH=""
VERSION=""
SKIP_BUILD=0
SKIP_RUNTIME_VERIFY=0
KEEP_STAGING=0

usage() {
  cat <<'EOF'
Usage:
  publish/mac/publish-mac.sh --arch <arm64|amd64|universal> [options]

Options:
  --arch <arm64|amd64|universal> Target architecture (required)
  --version <ver>                Package version (default: read from wails.json)
  --skip-build                   Skip frontend and Wails build steps
  --skip-runtime-verify          Skip runtime hash verification
  --keep-staging                 Keep assembled .app bundle in publish/staging/mac
  -h, --help                     Show help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --arch)
      ARCH="${2:-}"
      shift 2
      ;;
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    --skip-runtime-verify)
      SKIP_RUNTIME_VERIFY=1
      shift
      ;;
    --keep-staging)
      KEEP_STAGING=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "[ERROR] Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$ARCH" ]]; then
  echo "[ERROR] --arch is required" >&2
  usage
  exit 1
fi

if [[ "$ARCH" != "amd64" && "$ARCH" != "arm64" && "$ARCH" != "universal" ]]; then
  echo "[ERROR] unsupported arch: $ARCH (expected arm64, amd64, or universal)" >&2
  exit 1
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "[ERROR] this script must run on macOS host" >&2
  exit 1
fi

if ! command -v wails >/dev/null 2>&1; then
  if command -v go >/dev/null 2>&1 && [[ -d "$(go env GOPATH 2>/dev/null)/bin" ]]; then
    export PATH="$(go env GOPATH)/bin:$PATH"
  elif [[ -d "$HOME/go/bin" ]]; then
    export PATH="$HOME/go/bin:$PATH"
  fi
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[ERROR] required command not found: $1" >&2
    exit 1
  fi
}

require_cmd python3
require_cmd ditto
require_cmd wails

if [[ -z "$VERSION" ]]; then
  VERSION="$(python3 - "$ROOT_DIR/wails.json" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as f:
    data = json.load(f)
version = (((data or {}).get("info") or {}).get("productVersion") or "").strip()
if not version:
    raise SystemExit("productVersion missing in wails.json")
print(version)
PY
)"
fi

ARCH_LABEL="$ARCH"
if [[ "$ARCH" == "amd64" ]]; then
  ARCH_LABEL="x86_64"
fi

TARGET="darwin-$ARCH"
APP_BIN_DIR="$ROOT_DIR/build/bin"
CHROME_README_SRC="$ROOT_DIR/chrome/README.md"
CONFIG_INIT_SRC="$ROOT_DIR/publish/config.init.mac.yaml"
ZIP_NAME="AntBrowser-${VERSION}-macos-${ARCH_LABEL}.zip"
DMG_NAME="AntBrowser-${VERSION}-macos-${ARCH_LABEL}.dmg"
APP_EXPORT="$OUTPUT_DIR/AntBrowser-${VERSION}-macos-${ARCH_LABEL}.app"
STAGE_DIR="$STAGING_ROOT/$TARGET"
APP_STAGE="$STAGE_DIR/Ant Browser.app"

if [[ "$ARCH" == "universal" ]]; then
  WAILS_PLATFORM="darwin/universal"
  UNIVERSAL_BIN_DIR="$STAGING_ROOT/universal_bin"
  mkdir -p "$UNIVERSAL_BIN_DIR"
  XRAY_SRC="$UNIVERSAL_BIN_DIR/xray"
  SINGBOX_SRC="$UNIVERSAL_BIN_DIR/sing-box"
  lipo -create "$ROOT_DIR/bin/darwin-arm64/xray" "$ROOT_DIR/bin/darwin-amd64/xray" -output "$XRAY_SRC"
  lipo -create "$ROOT_DIR/bin/darwin-arm64/sing-box" "$ROOT_DIR/bin/darwin-amd64/sing-box" -output "$SINGBOX_SRC"
  chmod +x "$XRAY_SRC" "$SINGBOX_SRC"
else
  WAILS_PLATFORM="darwin/$ARCH"
  RUNTIME_DIR="$ROOT_DIR/bin/$TARGET"
  XRAY_SRC="$RUNTIME_DIR/xray"
  SINGBOX_SRC="$RUNTIME_DIR/sing-box"
fi

find_built_app_bundle() {
  python3 - "$APP_BIN_DIR" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
if not root.is_dir():
    sys.exit(0)

candidates = [p for p in root.iterdir() if p.is_dir() and p.suffix == ".app"]
if not candidates:
    sys.exit(0)

candidates.sort(key=lambda p: p.stat().st_mtime, reverse=True)
print(candidates[0])
PY
}

manifest_has_target() {
  python3 - "$ROOT_DIR/publish/runtime-manifest.json" "$TARGET" <<'PY'
import json
import sys

manifest_path = sys.argv[1]
target = sys.argv[2]

with open(manifest_path, "r", encoding="utf-8") as f:
    data = json.load(f)

for item in data.get("files", []):
    if target in (item.get("targets") or []):
        print("yes")
        raise SystemExit(0)

raise SystemExit(1)
PY
}

echo "========================================"
echo "  Ant Browser macOS Publish"
echo "========================================"
echo "Target : $TARGET"
echo "Version: $VERSION"
echo "Root   : $ROOT_DIR"
echo

if [[ ! -f "$XRAY_SRC" || ! -f "$SINGBOX_SRC" ]]; then
  echo "[ERROR] runtime files missing for $TARGET" >&2
  echo "        expected: $XRAY_SRC and $SINGBOX_SRC" >&2
  exit 1
fi

if [[ ! -f "$CONFIG_INIT_SRC" ]]; then
  echo "[ERROR] mac config template missing: $CONFIG_INIT_SRC" >&2
  exit 1
fi

if [[ "$SKIP_RUNTIME_VERIFY" -ne 1 ]]; then
  if manifest_has_target >/dev/null 2>&1; then
    bash "$ROOT_DIR/tools/runtime/verify-runtime.sh" "$TARGET"
  else
    echo "[WARN] runtime manifest does not yet define $TARGET, skipping hash verification"
  fi
else
  echo "[WARN] runtime verification skipped"
fi

if [[ "$SKIP_BUILD" -ne 1 ]]; then
  echo "[1/4] Installing frontend dependencies..."
  (cd "$ROOT_DIR/frontend" && npm ci --prefer-offline --no-audit --no-fund)

  echo "[2/4] Building frontend assets..."
  (cd "$ROOT_DIR/frontend" && npm run build:clean)

  echo "[3/4] Building macOS app bundle with Wails..."
  (
    cd "$ROOT_DIR"
    wails build -s -platform "$WAILS_PLATFORM" -o ant-chrome
  )
else
  echo "[WARN] skipping build step"
fi

APP_SOURCE="$(find_built_app_bundle)"
if [[ -z "$APP_SOURCE" || ! -d "$APP_SOURCE" ]]; then
  echo "[ERROR] failed to locate built .app bundle under $APP_BIN_DIR" >&2
  exit 1
fi

echo "[4/4] Assembling macOS app bundle..."
rm -rf "$APP_STAGE" "$APP_EXPORT"
mkdir -p "$STAGE_DIR" "$OUTPUT_DIR"
ditto "$APP_SOURCE" "$APP_STAGE"

APP_MACOS_DIR="$APP_STAGE/Contents/MacOS"
if [[ ! -d "$APP_MACOS_DIR" ]]; then
  echo "[ERROR] invalid app bundle layout, missing: $APP_MACOS_DIR" >&2
  exit 1
fi

mkdir -p "$APP_MACOS_DIR/bin"
cp "$XRAY_SRC" "$APP_MACOS_DIR/bin/xray"
cp "$SINGBOX_SRC" "$APP_MACOS_DIR/bin/sing-box"
cp "$CONFIG_INIT_SRC" "$APP_MACOS_DIR/config.yaml"
chmod +x "$APP_MACOS_DIR/bin/xray" "$APP_MACOS_DIR/bin/sing-box"

if [[ -f "$CHROME_README_SRC" ]]; then
  mkdir -p "$APP_MACOS_DIR/chrome"
  cp "$CHROME_README_SRC" "$APP_MACOS_DIR/chrome/README.md"
fi

ditto "$APP_STAGE" "$APP_EXPORT"
rm -f "$OUTPUT_DIR/$ZIP_NAME"
ditto -c -k --sequesterRsrc --keepParent "$APP_EXPORT" "$OUTPUT_DIR/$ZIP_NAME"

if command -v hdiutil >/dev/null 2>&1; then
  DMG_STAGE="$STAGING_ROOT/dmg_${TARGET}"
  rm -rf "$DMG_STAGE" "$OUTPUT_DIR/$DMG_NAME"
  mkdir -p "$DMG_STAGE"
  ditto "$APP_EXPORT" "$DMG_STAGE/Ant Browser.app"
  ln -s /Applications "$DMG_STAGE/Applications"
  hdiutil create -volname "Ant Browser" -srcfolder "$DMG_STAGE" -ov -format UDZO "$OUTPUT_DIR/$DMG_NAME" >/dev/null
  rm -rf "$DMG_STAGE"
fi

echo "Artifacts generated:"
echo "  - $APP_EXPORT"
echo "  - $OUTPUT_DIR/$ZIP_NAME"
if [[ -f "$OUTPUT_DIR/$DMG_NAME" ]]; then
  echo "  - $OUTPUT_DIR/$DMG_NAME"
fi

if [[ "$KEEP_STAGING" -ne 1 ]]; then
  rm -rf "$APP_STAGE"
fi

echo "Done."
