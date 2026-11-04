# Edit board, kolom Draft, chat Brainstorming — Design

Tanggal: 2026-11-04. Lanjutan slice tiket (modal, review, flow, Agent/QA). Teks UI bahasa Inggris, dokumen ini bahasa Indonesia.

## Tujuan

Pemilik (satu user, lokal) ingin merencanakan seluruh produk bersama Claude **sebelum** tiket ditulis
rapi. Hari ini tiket hanya bisa dibuat satu per satu di kolom Business. Tiga tambahan:

1. **Edit board**: ganti nama, deskripsi, dan tambah/hapus folder dari halaman board.
2. **Kolom Draft** (backlog) di kiri Business: tempat tiket kasar, boleh bolong.
3. **Chat Brainstorming** per board: modal chat dengan `claude -p` yang sudah tahu konteks board,
   hasilnya satu **dokumen rencana MD** (mudah dibaca manusia) dan kartu-kartu Draft di board.

Tiket Draft baru dilengkapi (DoD, flow, review, Approved) nanti, bersama Claude, setelah diseret
ke Business dan dikerjakan. Alur review/approve yang sudah ada tidak berubah.

## Non-goals

Menjalankan tugas Agent/QA oleh Claude, integrasi drawdb, multi-user, melengkapi tiket Draft di
dalam chat (itu tugas alur review yang ada), diff/merge dokumen secara visual.

## Anti-goals (gagal walau "jalan")

- Token membengkak: seluruh DB atau seluruh riwayat dikirim ulang tiap pesan.
- Claude mengubah atau menghapus kartu non-Draft, mengubah kode di folder board, atau menyentuh DB mentah.
- Rencana lama tertimpa tanpa jalan kembali.
- Chat memblokir UI atau tidak bisa dibatalkan.

## 1. Edit board

- Header halaman board dapat tombol/menu **Edit board**. Memakai `BoardModal` dan
  `PUT /api/boards/{id}` yang sudah ada (nama, deskripsi, folder satu per baris, picker folder).
  Tidak ada API baru. Validasi folder tetap sama seperti saat membuat board.
- Setelah simpan, header board (nama, chip folder) langsung ikut berubah.

## 2. Kolom Draft

- Status baru `draft`, urutan kolom: **Draft, Business, Agent, QA, Done**. Titik warna Draft abu-abu.
- Kartu Draft: judul + deskripsi, boleh singkat/bolong. **Tidak ada review dan tidak bisa Approved**;
  server menolak `review`/`approve` untuk kartu `draft` (bukan hanya UI menyembunyikan tombol).
- Drag Draft → Business membuka jalur normal (review, flow, Approved). Business → Draft diizinkan
  (mengembalikan ke backlog) dan membatalkan Approved seperti perubahan isi lainnya. Review, flow,
  dan DoD kartu **tetap disimpan** (hanya disembunyikan di Draft) dan muncul lagi saat kartu diseret
  ke Business (diputuskan user); hanya status Approved yang dibatalkan, jadi review ulang diperlukan
  sebelum Approved lagi. Draft tidak
  boleh langsung ke Agent/QA/Done: gerbang yang sudah berlaku untuk Business tetap berlaku
  lewat Business.
- Kartu Draft dibuat oleh Claude dari chat; kolom Business tetap punya "+ Add card". Draft tidak
  punya "+ Add card" manual (keputusanku; mudah ditambah bila diminta).
- Klik kartu Draft membuka modal tiket yang sama; panel review/flow/Approved disembunyikan.

## 3. Chat Brainstorming

### UI
- Di header board, di sebelah chip folder, tombol **Brainstorming**. Klik membuka modal chat:
  daftar pesan, input, tombol Send dan **Stop**, tab/tombol untuk melihat **Plan** (dokumen MD) dan
  riwayat versinya. Balasan Claude di-stream per bagian (pola live log yang sudah ada di review).
- Riwayat chat board tampil lagi saat modal dibuka ulang (satu percakapan berkelanjutan per board;
  tombol "New conversation" memulai sesi Claude baru tanpa menghapus transkrip lama atau rencana).

### Perilaku Claude (system prompt kita sendiri)
- Ikut pola brainstorming: tanya satu pertanyaan per giliran, ulang-rangkum pemahaman, sebut tebakan
  sebagai tebakan, usulkan konkret kalau diminta menebak, jangan membangun apa pun. Bahasa mengikuti user.
- **Kita tidak bergantung pada skill `/superpowers:brainstorming` terpasang.** Review sudah memakai
  `--setting-sources project` (plugin user tidak dimuat), jadi instruksi brainstorming ditulis ringkas
  di system prompt sendiri. (Keputusanku; menguji skill di mode `-p` tidak diperlukan.)
- Tujuan akhir sesi: dokumen rencana dan kartu Draft (lihat bawah). Tiap tiket Draft berisi judul dan
  deskripsi singkat (apa dan mengapa), tanpa memaksa DoD atau swimlane.

### Konteks dan token (aturan keras)
- **Selalu dikirim (kecil):** nama board, deskripsi, folder, dan indeks tiket (id, judul, status
  saja). Teks ini dijaga **identik antar giliran selama tidak berubah** supaya prompt cache kena.
  Indeks hanya dibangun ulang saat tiket berubah, dan perubahan dikirim lewat pesan user
  ("Tiket berubah: …"), bukan dengan menulis ulang system prompt.
- **Dibaca bila perlu:** detail satu tiket lewat subcommand baca-saja pada binary server (mis.
  `server ticket <id>`), membuka SQLite `mode=ro`. `claude -p` hanya boleh menjalankan subcommand itu
  (`--allowedTools "Bash(<path-binary> ticket:*)"` dll.), plus Read/Grep/Glob untuk folder board
  (`--add-dir`, read-only). Panjang output tiap subcommand dibatasi (deskripsi dipotong di daftar,
  penuh hanya di `ticket <id>`).
- **Lanjut sesi:** simpan `session id` Claude per board, pakai `--resume` (jadi tanpa
  `--no-session-persistence` untuk fitur ini). Riwayat tidak dikirim ulang manual.
- Batas per giliran: timeout 5 menit, jumlah giliran tool dibatasi (`--max-turns`), satu giliran aktif
  per board (pesan kedua saat berjalan ditolak 409). Stop menghentikan prosesnya.

### Dokumen rencana
- Satu dokumen MD **per board**, diperbarui tiap sesi (bukan dokumen baru per sesi). Isi: gambaran produk,
  tujuan/non-tujuan, daftar tiket (judul + ringkasan, merujuk id kartu Draft), pertanyaan terbuka.
- Disimpan sebagai file per board di folder data aplikasi; Claude diberi Read/Edit/Write **hanya**
  pada file itu (Edit hemat token dibanding menulis ulang seluruhnya). Setelah tiap giliran, bila
  isinya berubah, server menyimpan **versi baru** (tabel versi, pola yang sama dengan versi isi kartu)
  sehingga bisa dikembalikan. Tab Plan di modal menampilkan versi terbaru dan daftar versi.
- Yang resmi sebagai "rencana" hanya dokumen ini; transkrip chat adalah riwayat.
- **Restore (diputuskan user):** tiap versi punya tombol Restore. Restore menulis isi versi itu ke file
  rencana **dan** menambah versi baru (riwayat tidak pernah dihapus). Ditolak (409) saat giliran
  Claude sedang berjalan. **Edit manual MD di UI tidak ada** (diputuskan user: ditunda); perubahan
  rencana hanya lewat chat atau Restore.

### Kartu Draft dari Claude
- Lewat subcommand tulis yang divalidasi server, bukan DB mentah: `draft add --title --description`,
  `draft update <id> ...`. Perintah ini **hanya** berlaku untuk kartu `draft` di board sesi itu;
  id non-Draft ditolak. Tidak ada perintah hapus (hapus dilakukan user di UI).
- Kartu muncul di kolom Draft langsung (UI memuat ulang daftar kartu setelah tiap giliran).

### Data baru (garis besar; detail skema diserahkan ke builder)
Transkrip pesan per board, `session id` Claude per board, versi dokumen rencana per board.
Migrasi idempoten mengikuti pola `addColumn`/`CREATE TABLE IF NOT EXISTS` di `store.go`.

## Diserahkan ke builder

Bentuk skema tabel, format streaming ke browser (mengikuti live log review), nama subcommand persis,
cara mencari path binary untuk `allowedTools`, apakah kolom `status` di SQLite punya CHECK yang perlu
dimigrasi untuk nilai `draft`, tampilan Plan/riwayat versi di modal, tes (store + api + satu tes
subcommand baca-saja yang menolak penulisan ke kartu non-Draft).

## Urutan pengerjaan

1. Edit board (kecil, mandiri).
2. Kolom Draft (status, urutan, penolakan review/approve di server, UI).
3. Chat Brainstorming (modal, sesi, konteks hemat token, dokumen + versi, subcommand, kartu Draft).

## Keputusan yang diambil asisten (bukan dari percakapan)

- Draft tanpa "+ Add card" manual; Business → Draft diizinkan; Draft tidak boleh lompat ke Agent/QA/Done.
- Instruksi brainstorming ditanam di system prompt sendiri, tidak memakai skill terpasang.
- Dokumen rencana berupa file yang diedit Claude dengan Edit, versi disimpan server setelah tiap giliran.
- Timeout 5 menit, `--max-turns` terbatas, satu giliran aktif per board.
- Subcommand `ticket`/`draft` memakai paket `store` yang sama dengan server (validasi satu tempat), SQLite
  WAL + `busy_timeout`. Id board diberikan server lewat env var saat memanggil `claude`, tidak bisa
  diubah Claude; penulisan ke kartu board lain atau non-Draft ditolak di lapisan store.
- Pergerakan kartu: memindah kartu ke Draft ditolak (409) selama review kartu itu berjalan. Bila
  `draft update` Claude kena kartu yang sudah dipindah, perintah gagal dengan pesan jelas dan UI
  memuat ulang kartu setelah giliran selesai.
- "Tiket berubah": server membandingkan indeks sekarang dengan indeks terakhir yang dikirim ke sesi
  itu (disimpan per board, diperbarui juga setelah perubahan yang dibuat Claude sendiri, jadi tidak
  dikirim ulang). Hanya selisih yang dikirim.
- "New conversation": pesan pertama sesi baru menyuruh Claude membaca file rencana. Mengubah folder
  board tidak membatalkan sesi; bila `--resume` gagal, server otomatis memulai sesi baru. Menghapus
  board menghapus transkrip, session id, versi, dan file rencana.
- Stop/timeout/error: balasan parsial tetap disimpan di transkrip, dan versi rencana disimpan bila
  file berubah. Bila sesi tidak bisa dilanjutkan, jatuh ke sesi baru (lihat di atas).
