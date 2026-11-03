# One Piece — Slice 1: Login + Kanban Board

Tanggal: 2026-11-03 · Status: DRAFT, menunggu persetujuan

## Latar belakang & tujuan besar

One Piece adalah AI workflow pribadi untuk membuat product, dipakai satu orang (pemilik). Semua pekerjaan AI dijalankan lewat `claude -p`.

Masalah yang diselesaikan:
1. Tiket dari product sering tidak jelas. One Piece nanti mereview tiket dan mewajibkan DoD + swimlane sebelum tiket dianggap siap.
2. Pemilik punya 5-10 session cmux berjalan sekaligus dan harus mencari-cari. One Piece jadi satu tempat untuk melihat apa yang sedang dan sudah dikerjakan Agent.
3. Nanti swim-lanes (`HEPHAESTUS/swim-lanes`) dan drawdb (`HEPHAESTUS/drawdb`, untuk ERD) ikut tampil di dalam One Piece.

Alur besar (BUKAN bagian slice 1): tiket Business → direview & di-approve → tiket Agent (task + swimlane technical flow, dieksekusi setelah di-approve, dengan live log) dan tiket QA (test case, dijalankan setelah Agent selesai) → Done.

## Slice 1: ruang lingkup

Goals:
- **Login** satu user. Alasan login tetap ada: One Piece nanti mengakses folder lokal.
- **Board**: buat, ubah, hapus. 1 board = 1 product. Tiap board punya nama dan **daftar folder** sendiri (satu product sering melibatkan beberapa folder, mis. frontend + backend).
- **Kanban** per board dengan kolom tetap: Business → Agent → QA → Done. Kartu (judul + deskripsi) dibuat manual, digeser manual antar kolom.
- **Dashboard (home)**: daftar semua board. Ada area "Sedang dikerjakan Agent" yang di slice ini kosong/placeholder.

Non-goals (slice berikutnya, jangan dibangun sekarang):
- Review tiket / DoD / swimlane wajib, tiket Agent, tiket QA.
- Menjalankan `claude -p`, live log, halaman detail task Agent.
- Integrasi swim-lanes dan drawdb.
- Multi-user, role, registrasi, reset password.
- Kolom kustom, label, assignee, due date.

Anti-goals:
- Jangan membuat One Piece bisa diakses tanpa login atau dari jaringan lain secara tidak sengaja.
- Jangan over-engineer: ini alat pribadi, desain boleh diubah setelah terlihat jadi.

## Keputusan pemilik

- Stack: React (frontend) + Go (backend).
- Satu user saja.
- 1 board = 1 product, daftar folder milik board.
- Satu tiket bisa menyentuh beberapa folder (dipilih dari daftar folder board; dipakai di slice Agent nanti).
- Dashboard: daftar board + task Agent yang sedang jalan. Detail task Agent nanti: daftar task di kiri, log Claude di kanan.
- Kolom kanban mengikuti alur Business → Agent → QA → Done.
- Visual companion/mockup ditolak; pemilik lebih suka melihat hasil jadi lalu merevisi.

## Keputusan saya (bisa diubah)

- Backend: Go, `net/http` standar; SQLite lewat driver pure-Go (`modernc.org/sqlite`), tanpa cgo.
- Frontend: React + Vite. Saat produksi lokal, binary Go menyajikan hasil build.
- Password: bcrypt. Akun dibuat lewat perintah CLI/env saat setup (tanpa halaman registrasi). Sesi: cookie `HttpOnly`, `SameSite=Lax`.
- Server hanya listen di `127.0.0.1`.
- Daftar folder board: wajib path absolut yang ada di disk; divalidasi saat disimpan. Daftar ini nanti jadi allowlist folder untuk `claude -p` (dipakai penuh di slice Agent).
- Menghapus board ikut menghapus kartunya, dengan konfirmasi.
- Layout repo: `ONE-PIECE/backend`, `ONE-PIECE/frontend`, `docs/`. Satu skrip start untuk menyalakan semuanya.

## Diserahkan ke builder

Port, struktur paket Go, skema tabel, library drag-and-drop, styling/tema, dan strategi test (cukup test API untuk login, board, dan kartu).

## Catatan

Folder ini belum git repo; commit dokumen dilakukan setelah `git init` bila pemilik mau.
