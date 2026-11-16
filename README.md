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
- **Discuss** (tombol di modal tiket, di **semua kolom**): chat dengan Claude yang menjalankan git untuk kamu. Business = setup terpandu, Done = closeout terpandu, kolom lain = asisten git umum untuk tiket itu.
  - *Business* (setup): Claude menanyakan repo (ketik `@` untuk memilih; repo git diberi tag `repo`) dan base branch tiap repo, lalu membuat `feature/{ident}` dan mencatatnya di Branching.
  - *Done* (closeout): fase 1, tiap repo diintegrasikan ke `feature/{ident}-dev` (dibuat dari branch dev bersama) sesuai **mode** repo itu (`merge` atau `cherry-pick`, diatur di Branching) tanpa push apa pun; fase 2, hanya bila SEMUA repo bersih: push kedua branch, lalu PR lewat `gh` untuk remote GitHub (remote lain berhenti di status `pushed`, tanpa `gh`). Konflik: Claude `--abort`, melapor, dan menunggu kamu; ia tidak menyelesaikan konflik. Server menolak push `-dev` selama ada repo yang belum terintegrasi atau `blocked`.
  - **Aturan aman**: Claude tidak diberi shell. Ia hanya bisa memakai subcommand server (`branches`, `branch-plan`, `branch-status`, `branch-pr`, `git`, `gh`) yang diperiksa `internal/gitx`: tanpa force-push, tanpa push ke `dev`/`main`/`master`, tanpa `reset`/`clean`/`rebase`/hapus branch; `merge`/`cherry-pick`/`checkout`/`pull --ff-only` hanya dari working tree bersih (abort dikecualikan) dan hanya pada branch tiket itu; `cherry-pick -x` hanya untuk commit milik tiket (non-merge di `base..feature`) yang belum di-pick; `stash` hanya dengan pesan berisi identifier tiket, `stash pop` hanya untuk stash tiket itu, `stash drop/clear` tidak ada; dev atau `-dev` tidak pernah di-merge ke feature; `gh` hanya `pr create`/`pr view`. Bila ada yang aneh (konflik, perubahan belum commit, push gagal) Claude berhenti dan bertanya.
- **Decision log**: yang kamu sepakati dengan Claude di Discuss disimpan Claude (`decision`) sebagai catatan bertanggal di tiket (bisa kamu tambah/hapus), dan dikirim di pesan pertama Discuss baru, Run, dan QA untuk tiket itu. Perubahan setelah integrasi (commit Run baru di repo yang sudah `merged_dev`/`pushed`/`pr_open`) mengembalikan repo ke `branched`, sehingga harus diintegrasikan lagi sebelum push.
- **Run sadar-git**: bila tiket punya repo berstatus `branched`, Run menolak (409) jika repo tidak sedang di branch feature-nya, Claude hanya boleh menulis di repo terdaftar, dan tiap tugas Agent yang berhasil di-commit sendiri sebagai `{ident tugas}: {judul}`. Commit gagal (mis. hook) membuat tugas kembali ke To Do dan Run All berhenti. Perubahan yang belum di-commit di repo itu sebelum Run ikut masuk commit tugas pertama, jadi bersihkan dulu.

## Belum ada (slice berikutnya)

Integrasi drawdb.

Desain: `docs/superpowers/specs/`. Rencana: `docs/superpowers/plans/`.

## Generate Berjalan di Background

Generate Agent Tasks, QA Test Cases, Flow, dan Description berjalan sebagai job di server. Modal boleh ditutup (atau pindah tiket/board): saat dibuka lagi tampil loading kalau masih jalan, atau hasilnya kalau sudah selesai. Toast muncul saat job selesai, dan kartu kanban menampilkan spinner selama ada job yang jalan. Regenerate tidak menghapus: task To Do yang lama dicoret (*Superseded*, bisa di-Restore), task baru ditambahkan.

## Claude Review sebagai Diskusi

Tombol **Review With Claude** di sidebar tiket (kolom Business) membuka chat dengan Claude. Claude membaca tiket dan kode, lalu **langsung memperbaiki** judul, deskripsi, dan Business Flow lewat perintah terbatas (`ticket-edit`, `flow-add|update|delete`); versi sebelumnya disimpan dan bisa di-**Undo** dari baris perubahan di chat. Di akhir setiap giliran Claude mencatat verdict (`pass` atau `needs_work` + poin yang masih terbuka); Approve terbuka setelah verdict `pass` untuk versi tiket yang sekarang. Review tidak lagi jalan otomatis saat save. Selama Claude bekerja, editor judul, deskripsi, dan flow terkunci, lalu tiket dimuat ulang. Chat berjalan di background (toast + spinner di kartu), mendukung paste screenshot, dan tidak boleh menjalankan git atau mengubah file produk.
