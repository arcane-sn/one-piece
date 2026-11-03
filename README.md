# One Piece

AI workflow pribadi untuk membuat product. Slice 1: login + dashboard + kanban board.

## Jalankan

```sh
# sekali saja: buat satu-satunya user (password diminta tanpa echo, min. 8 karakter)
cd backend && go run ./cmd/server create-user <username> && cd ..

./start.sh        # http://127.0.0.1:5300
```

Butuh Go ≥ 1.22 dan Node. `start.sh` membangun frontend bila `frontend/dist` belum ada.

Development: jalankan backend (`cd backend && go run ./cmd/server`) dan frontend (`cd frontend && npm run dev`, buka http://127.0.0.1:5301). Tes: `cd backend && go test ./...`.

## Catatan keamanan

- Server hanya mau listen di loopback (`127.0.0.1`, `localhost`, `::1`); alamat lain ditolak saat start.
- **Repo ini public.** Jangan commit password, token, `.env`, atau file `*.db` (sudah di `.gitignore`).

## Belum ada (slice berikutnya)

Review tiket (DoD + swimlane), tiket Agent/QA, eksekusi `claude -p` dengan live log, integrasi swim-lanes dan drawdb.

Desain: `docs/superpowers/specs/`. Rencana: `docs/superpowers/plans/`.
