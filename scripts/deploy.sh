#!/usr/bin/env bash
# Despliega la versión actual del repo: compila la imagen del relé con la versión de git (así NIP-11, el
# panel y la página de presentación muestran qué código está en marcha) y recrea el contenedor.
#
#   ./scripts/deploy.sh            # recompila y recrea el relé
#   ./scripts/deploy.sh --caddy    # además recrea Caddy (hace falta tras editar el Caddyfile o .env)
#
# La versión es `git describe --tags --always --dirty`: una etiqueta si la hay (p. ej. v1.2.0), si no el
# hash corto del commit, y «-dirty» si hay cambios sin commitear.

set -euo pipefail
cd "$(dirname "$0")/.."

export VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
echo "versión: $VERSION"

docker compose up -d --build relay
if [ "${1:-}" = "--caddy" ]; then
  docker compose up -d --force-recreate caddy
fi
docker compose ps
