#!/bin/sh
# Build the frontend if needed, then run One Piece on http://127.0.0.1:5300
set -e
cd "$(dirname "$0")"

if [ ! -d frontend/dist ]; then
  (cd frontend && npm ci && npm run build)
fi

cd backend
[ -f one-piece.db ] || echo "Belum ada user. Buat dulu: (cd backend && go run ./cmd/server create-user <username>)"
echo "One Piece: http://127.0.0.1:5300"
exec go run ./cmd/server -static ../frontend/dist
