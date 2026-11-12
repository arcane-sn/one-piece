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

## Identifier, Branching, dan Discuss

- **Identifier**: tiap board punya *key* 2-5 huruf (otomatis dari nama, mis. `MID`; bisa diubah di Edit Board). Tiket dapat `MID-12`, tugas Agent `MID-12-T3`, kasus QA `MID-12-QA1`. Nomor dibuat sistem, tidak pernah dipakai ulang, dan tidak berubah walau key diganti (key baru hanya untuk tiket berikutnya). Identifier ikut dikirim ke Claude di semua prompt.
- **Branching** (di modal tiket, di bawah Business Flow): satu baris per repo: base branch → `feature/{ident}`, branch `-dev`, status (`planned`, `branched`, `merged_dev`, `pr_open`, `blocked`), dan link PR. Awalan branch dan branch dev bersama diatur per board (default `feature/` dan `dev`). Repo harus berada di dalam folder board (folder pertama boleh berupa master folder berisi banyak repo).
- **Discuss** (tombol di modal tiket, hanya saat tiket di **Business** atau **Done**): chat dengan Claude yang menjalankan git untuk kamu.
  - *Business* (setup): Claude menanyakan repo (ketik `@` untuk memilih; repo git diberi tag `repo`) dan base branch tiap repo, lalu membuat `feature/{ident}` dan mencatatnya di Branching.
  - *Done* (closeout): per repo Claude membuat `feature/{ident}-dev` dari branch dev bersama, me-merge feature ke situ, push keduanya, dan membuka PR `-dev` → dev lewat `gh` (perlu `gh` terpasang dan sudah login).
  - **Aturan aman**: Claude tidak diberi shell. Ia hanya bisa memakai subcommand server (`branches`, `branch-plan`, `branch-status`, `branch-pr`, `git`, `gh`) yang diperiksa `internal/gitx`: tanpa force-push, tanpa push ke `dev`/`main`/`master`, tanpa `reset`/`clean`/`rebase`/hapus branch; `merge` hanya ke branch `-dev` tiket itu dan hanya dari working tree bersih; `gh` hanya `pr create`/`pr view`. Bila ada yang aneh (konflik, perubahan belum commit, push gagal) Claude berhenti dan bertanya.
- **Run sadar-git**: bila tiket punya repo berstatus `branched`, Run menolak (409) jika repo tidak sedang di branch feature-nya, Claude hanya boleh menulis di repo terdaftar, dan tiap tugas Agent yang berhasil di-commit sendiri sebagai `{ident tugas}: {judul}`. Commit gagal (mis. hook) membuat tugas kembali ke To Do dan Run All berhenti. Perubahan yang belum di-commit di repo itu sebelum Run ikut masuk commit tugas pertama, jadi bersihkan dulu.

## Belum ada (slice berikutnya)

Integrasi drawdb.

Desain: `docs/superpowers/specs/`. Rencana: `docs/superpowers/plans/`.

## Generate Berjalan di Background

Generate Agent Tasks, QA Test Cases, Flow, dan Description berjalan sebagai job di server. Modal boleh ditutup (atau pindah tiket/board): saat dibuka lagi tampil loading kalau masih jalan, atau hasilnya kalau sudah selesai. Toast muncul saat job selesai, dan kartu kanban menampilkan spinner selama ada job yang jalan. Regenerate tidak menghapus: task To Do yang lama dicoret (*Superseded*, bisa di-Restore), task baru ditambahkan.
