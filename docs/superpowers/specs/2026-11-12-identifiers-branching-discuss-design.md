# One Piece — Identifiers, Branching, and the Discuss session

Tanggal: 2026-11-12 · Status: DRAFT, menunggu persetujuan

## Latar belakang

One Piece sekarang punya: board, kanban Draft → Business → Agent → QA → Done, review tiket, Agent Task dan QA Test Case hasil Claude, tombol Run / Run All (Claude mengedit file di folder board), alur `[FIX]` saat QA gagal, dan chat Brainstorming per board.

Yang belum ada: **git**. Pemilik mengerjakan tiket di beberapa repo di dalam satu master folder, selalu lewat branch `feature/{tiket}`, dan masuk ke `dev` lewat PR dari branch `feature/{tiket}-dev`. Saat ini semua itu dikerjakan manual, dan tiket/task/test case tidak punya nama yang bisa dirujuk dari branch, commit, atau obrolan.

Dokumen ini mencakup tiga hal yang saling bergantung:

1. **Identifier** untuk tiket, Agent Task, dan QA Test Case.
2. **Discuss session**: satu chat per tiket yang membuka `claude -p` dan mengarahkan Claude menjalankan alur git.
3. **Branching**: data git per tiket, dan perilaku git di Run.

## Tujuan

- Tiap tiket, task, dan test case punya identifier unik yang dibuat One Piece, stabil, dan tampil di UI.
- Pemilik bisa menunjuk repo mana saja yang dikerjakan sebuah tiket dengan `@`, dan Claude membuat `feature/{tiket}` di repo itu dari branch dasar pilihan pemilik.
- Saat tiket selesai, Claude menyiapkan `feature/{tiket}-dev` dari `dev`, me-merge `feature/{tiket}` ke dalamnya, push, dan membuat PR ke `dev`.
- Keputusan branching tersimpan di tiket sebagai data terstruktur yang dipakai langkah berikutnya, bukan hanya tertulis di chat.
- Fleksibel: tiket tanpa Jira atau tanpa git tetap bisa dipakai seperti sekarang.

## Bukan tujuan

- Koneksi ke Jira. Pemilik menyalin isi tiket sendiri, dan kunci Jira **tidak** dipakai sebagai identifier.
- Banyak folder per board. Board hanya perlu **satu folder utama** (master folder); repo adalah folder-folder git di dalamnya.
- Review PR, menunggu CI, atau merge PR. One Piece berhenti setelah PR dibuat.
- Menangani konflik merge yang rumit secara otomatis (lihat Anti-tujuan).
- Mengubah cara kerja Run: Run tetap memakai folder utama board sebagai folder kerja.

## Anti-tujuan (dianggap gagal walau secara teknis jalan)

- Claude melakukan `push --force`, atau commit langsung ke `dev`, `main`, atau branch dasar.
- Claude mengubah repo yang tidak ditunjuk pemilik untuk tiket itu.
- Claude menimpa atau membuang pekerjaan yang belum ter-commit.
- Claude menebak saat ada konflik atau keadaan git yang tidak jelas, lalu melanjutkan.
- Identifier berubah setelah dipakai di nama branch atau pesan commit, atau dipakai ulang untuk item lain.

## Keputusan pemilik

- Claude menjalankan git sendiri: membuat branch, merge, dan push (termasuk push), tanpa menunggu persetujuan per langkah.
- Pola branch: `feature/{tiket}` dari branch dasar pilihan pemilik; untuk ke dev: `feature/{tiket}-dev` dibuat dari `dev`, lalu `feature/{tiket}` di-merge ke situ, lalu PR.
- Satu Discuss session per tiket, tersedia saat tiket di kolom Business dan Done.
- Repo ditunjuk dengan `@` di Discuss session, dan hasilnya disimpan di tiket.
- Board punya satu folder (master folder); Run memakai folder itu.
- Identifier dibuat otomatis oleh One Piece untuk tiket, task, dan test case.

## Keputusan saya (pemilik belum menyetujui; ditandai agar bisa dikoreksi)

1. Format identifier (lihat bagian Identifier).
2. Penomoran tidak pernah dipakai ulang, juga setelah item dihapus atau di-Regenerate.
3. Daftar pengaman git dan cara menegakkannya (lihat Keamanan).
4. Commit per Agent Task dilakukan oleh server (bukan Claude) setelah task selesai.
5. PR dibuat dengan `gh`; kalau tidak terpasang, berhenti dan beri tahu.
6. Nilai bawaan board: awalan `feature/`, nama branch dev `dev`.

## 1. Identifier

Format:

- Board punya **kunci** singkat huruf besar, 2-5 huruf, dibuat dari nama board saat dibuat (mis. "Midas" → `MID`). Bisa diedit di Edit Board; harus unik antar board. Mengedit kunci tidak mengubah identifier yang sudah ada (identifier disimpan, bukan dihitung ulang).
- Tiket: `{KUNCI}-{n}`, mis. `MID-12`. `n` naik per board dan tidak dipakai ulang.
- Agent Task: `{tiket}-T{n}`, mis. `MID-12-T3`. Task `[FIX]` memakai deret yang sama.
- QA Test Case: `{tiket}-QA{n}`, mis. `MID-12-QA2`.
- `n` untuk task dan test case naik per tiket dan per jenis, tidak dipakai ulang. Regenerate membuat nomor baru (melanjutkan), tidak memulai dari 1, supaya nomor yang sudah muncul di commit tidak berubah arti.

Data lama: tiket, task, dan test case yang sudah ada diberi identifier berurutan menurut waktu pembuatan (id) saat migrasi. Kunci board lama dibuat dari namanya.

Tampilan: identifier tampil kecil di kartu kanban, judul modal, baris daftar Agent Task / QA Test Case, dan modal item. Bisa dipakai di prompt Claude (review, generate, run, QA) supaya Claude merujuk item dengan nama yang sama.

Draft: tiket Draft juga mendapat identifier saat dibuat (agar tidak berubah saat dipindah).

## 2. Discuss session

Satu chat per tiket, mengikuti pola chat Brainstorming yang sudah ada (modal, pesan tersimpan, sesi `claude -p` yang bisa dilanjutkan, tombol stop / mulai ulang). Perbedaannya:

- **Tahap** ditentukan dari kolom tiket: di Business, tujuannya menyiapkan branching; di Done, menutup (branch `-dev` dan PR). Satu transkrip per tiket, system prompt mengikuti tahap. Di kolom lain (Draft, Agent, QA) tombolnya tidak ditampilkan.
- Claude bekerja di **folder utama board** dan boleh membaca (Read, Grep, Glob) serta menjalankan **hanya perintah `git` dan `gh`** (lihat Keamanan).
- Pemilik menyebut repo dengan `@` memakai picker yang sudah ada, diperluas: folder yang berisi `.git` ditandai dan ditampilkan lebih dulu. Mention ke subfolder di dalam repo diartikan sebagai repo induknya. Mention ke folder yang bukan repo: Claude berhenti dan memberi tahu, tidak menjalankan `git init`.
- Claude menyimpan hasil keputusan ke tiket lewat subcommand `agent` (pola yang sama dengan penulisan Draft dari Claude), bukan dengan menulis teks bebas.

Tahap Business, urutan yang diharapkan: tanyakan repo (jika belum di-`@`) → tanyakan branch dasar per repo (usulkan nilai bawaan board) → periksa kondisi repo → buat `feature/{tiket}` dari branch dasar dan checkout → catat di tiket.

Tahap Done, urutan yang diharapkan: untuk tiap repo terdaftar, `fetch` → buat `feature/{tiket}-dev` dari `dev` terbaru → merge `feature/{tiket}` ke dalamnya → push `-dev` (dan `feature/{tiket}` jika belum) → buat PR ke `dev` → catat link PR.

## 3. Branching (data di tiket)

Bagian terstruktur **Branching** di tiket, satu baris per repo:

| Kolom | Arti |
|---|---|
| `repo` | path absolut repo (di dalam folder utama board) |
| `baseBranch` | branch dasar pilihan pemilik |
| `featureBranch` | `feature/{tiket}` (awalan dari nilai bawaan board) |
| `devBranch` | `feature/{tiket}-dev` |
| `status` | `planned` → `branched` → `merged_dev` → `pr_open`; plus `blocked` dengan alasan |
| `prUrl` | link PR bila sudah ada |

- Diisi Claude lewat Discuss session; pemilik bisa melihat dan mengedit di modal tiket.
- Board punya nilai bawaan: awalan branch (`feature/`) dan nama branch dev (`dev`). Dipakai sebagai usulan di Discuss session, tidak memaksa.
- Nama branch dibuat dari identifier tiket (mis. `feature/MID-12`, `feature/MID-12-dev`), bukan dari judul.
- Tiket tanpa baris Branching tetap berfungsi persis seperti sekarang (tanpa pengecekan git).

## 4. Perilaku git di Run

Hanya berlaku untuk tiket yang punya baris Branching:

- **Sebelum Run / Run All**: untuk setiap repo terdaftar dengan status `branched`, server memeriksa branch aktif sama dengan `featureBranch`. Kalau tidak, Run ditolak dengan toast yang menyebut repo dan branch yang seharusnya. Server tidak mengganti branch sendiri.
- **Setelah Agent Task selesai**: server menjalankan git sendiri (bukan Claude) di tiap repo terdaftar yang berubah: `git add -A` lalu commit dengan pesan `{identifier task}: {judul task}`. Repo yang tidak berubah dilewati. Kegagalan commit menandai task sebagai Blocked dengan alasannya.
- **Akses tulis Claude saat Run** dibatasi ke repo terdaftar untuk tiket itu (bukan seluruh folder utama). Selama ini Run bebas di seluruh folder utama; untuk tiket dengan Branching, batasnya menyempit.
- Task `[FIX]` dan task biasa diperlakukan sama.

## Keamanan

Claude di Discuss session hanya boleh menjalankan `git` dan `gh`. Penegakan lewat daftar izin/tolak pada `claude -p` (`--allowedTools` / `--disallowedTools`), bukan hanya lewat prompt:

- Ditolak: `push --force` / `-f` / `--force-with-lease`, `reset --hard`, `clean`, `branch -D`, `checkout .` / `restore .` yang membuang perubahan, `rebase`, `push` ke `dev`, `main`, `master`, atau branch dasar, `gh pr merge`, dan penghapusan branch remote.
- Diizinkan: `status`, `fetch`, `branch` (buat), `switch` / `checkout` ke branch, `merge`, `push` branch fitur dan `-dev`, `log`, `diff`, `gh pr create` dan `gh pr view`.
- Aturan perilaku di prompt: berhenti dan bertanya jika ada perubahan belum ter-commit, branch dasar tidak ada, merge konflik, `gh` tidak terpasang, atau remote menolak. Claude tidak menebak dan tidak melanjutkan diam-diam.
- Server menolak menjalankan Discuss session jika board tidak punya folder utama yang valid.

Daftar tolak di atas adalah titik awal; builder harus memverifikasi sintaks pola izin yang benar-benar didukung `claude -p` di versi yang terpasang dan menyesuaikannya.

## Diserahkan ke builder

- Bentuk skema database dan migrasi (kolom/tabel baru untuk kunci board, identifier, dan Branching; migrasi data lama sesuai bagian Identifier).
- Cara menjalankan `claude -p` untuk Discuss session mengikuti pola `brainstorm.go` yang ada.
- Subcommand `agent` yang dipakai Claude untuk menulis Branching, mengikuti pola `agentcli.go`.
- UI: penempatan tombol Discuss, tampilan bagian Branching di modal tiket, tampilan identifier (selama konsisten dengan gaya yang ada).
- Deteksi repo untuk picker `@` (folder dengan `.git`, kedalaman pencarian wajar).
- Urutan pengerjaan yang disarankan: (1) Identifier, (2) data Branching + tampilan, (3) Discuss session, (4) perilaku git di Run. Tiap tahap bisa dipakai sendiri.

## Belum diputuskan / risiko

- Pola izin `Bash(...)` pada `claude -p` perlu dicek ke versi yang terpasang; kalau tidak bisa membatasi sampai level subcommand, fallback-nya adalah menjalankan git dari server lewat subcommand `agent` yang hanya mengekspos langkah-langkah yang diizinkan.
- Konflik merge di tahap Done: Claude berhenti dan melapor; menyelesaikannya adalah pekerjaan manual pemilik pada slice ini.
- Beberapa repo dalam satu tiket yang sebagian berhasil dan sebagian gagal (mis. repo A sudah PR, repo B konflik): status per repo disimpan terpisah, tidak ada rollback lintas repo.
