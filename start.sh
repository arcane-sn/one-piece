#!/bin/sh
# One Piece di http://127.0.0.1:5300, jalan di background.
#   ./start.sh          (re)start: rebuild frontend/backend bila ada perubahan, matikan yang lama
#   ./start.sh stop     matikan
#   ./start.sh logs     ikuti log
set -e
cd "$(dirname "$0")"
PORT=5300
RUN=.run
LOG=$RUN/one-piece.log
mkdir -p "$RUN"

stop() {
  pids=$(lsof -ti "tcp:$PORT" -sTCP:LISTEN 2>/dev/null || true)
  [ -n "$pids" ] && kill $pids 2>/dev/null && echo "One Piece dimatikan"
  return 0
}

case "${1:-}" in
  stop) stop; exit 0 ;;
  logs) exec tail -f "$LOG" ;;
esac

# frontend: build bila belum ada atau ada source yang lebih baru dari hasil build
cd frontend
[ -d node_modules ] || npm ci
if [ ! -f dist/index.html ] || [ -n "$(find src index.html package.json -newer dist/index.html 2>/dev/null | head -1)" ]; then
  echo "Build frontend…"
  npm run build >/dev/null
fi
cd ../backend
echo "Build backend…"
go build -o "../$RUN/one-piece" ./cmd/server
[ -f one-piece.db ] || echo "Belum ada user. Buat dulu: (cd backend && go run ./cmd/server create-user <username>)"

stop; sleep 1
nohup "../$RUN/one-piece" -static ../frontend/dist >"../$LOG" 2>&1 &
for _ in 1 2 3 4 5 6 7 8 9 10; do
  curl -s -o /dev/null "http://127.0.0.1:$PORT" && { echo "One Piece: http://127.0.0.1:$PORT   (./start.sh logs | ./start.sh stop)"; exit 0; }
  sleep 1
done
echo "Gagal start, lihat $LOG"; tail -5 "../$LOG"; exit 1
