#!/usr/bin/env bash
# Reconstruye static/admin/vendor/nostr.js (nostr-tools recortado a lo que necesita el inicio de sesión NIP-46 del
# panel). El panel no tiene paso de compilación y su política de contenido solo admite scripts del mismo sitio, así que
# el resultado se guarda en el repo. Necesita Node y red (descarga esbuild y nostr-tools en una carpeta temporal).
set -euo pipefail
cd "$(dirname "$0")/.."
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
NOSTR_TOOLS_VERSION="${NOSTR_TOOLS_VERSION:-2.10.4}"
cp tools/admin-vendor-entry.mjs "$TMP/entry.mjs"
(cd "$TMP" && npm init -y >/dev/null && npm install --silent "nostr-tools@${NOSTR_TOOLS_VERSION}" esbuild >/dev/null \
  && npx esbuild entry.mjs --bundle --format=esm --minify --target=es2020 --legal-comments=none --outfile=nostr.js >/dev/null)
mkdir -p static/admin/vendor
cp "$TMP/nostr.js" static/admin/vendor/nostr.js
echo "static/admin/vendor/nostr.js: $(wc -c < static/admin/vendor/nostr.js) bytes (nostr-tools ${NOSTR_TOOLS_VERSION})"
