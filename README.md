# One Piece

AI workflow pribadi untuk membuat product. Slice 1: login + dashboard + kanban board.

## Jalankan

```sh
# sekali saja: buat satu-satunya user (password diminta tanpa echo, min. 8 karakter)
cd backend && go run ./cmd/server create-user <username> && cd ..

./start.sh        # http://127.0.0.1:5300
```

Butuh Go ≥ 1.26 (syarat dependency sqlite) dan Node. `start.sh` membangun frontend bila `frontend/dist` belum ada.

Development: jalankan backend (`cd backend && go run ./cmd/server`) dan frontend (`cd frontend && npm run dev`, buka http://127.0.0.1:5301). Tes: `cd backend && go test ./...`.

## Catatan keamanan

- Server hanya mau listen di loopback (`127.0.0.1`, `localhost`, `::1`); alamat lain ditolak saat start.
- **Repo ini public.** Jangan commit password, token, `.env`, atau file `*.db` (sudah di `.gitignore`).

## Sudah ada

Login, dashboard, kanban, review tiket oleh Claude (live log + approval gate), Business Flow (swimlane), tugas Agent dan kasus QA hasil generate, edit board dari halaman board, kolom Draft (backlog), eksekusi tugas Agent oleh `claude -p` (tombol Run di modal tugas; Claude mengedit file di folder pertama board, tanpa shell, dengan live log; Run All per tiket), eksekusi kasus QA oleh `claude -p` (Claude membaca kode, verdict pass/fail), dan chat Brainstorming per board (dokumen rencana MD berversi + kartu Draft dari Claude). Dokumen rencana disimpan di `backend/plans/` (di-ignore git).

## Belum ada (slice berikutnya)

Integrasi drawdb.

Desain: `docs/superpowers/specs/`. Rencana: `docs/superpowers/plans/`.
