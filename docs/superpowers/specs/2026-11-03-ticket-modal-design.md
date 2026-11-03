# Ticket modal, Claude review, swimlanes, Agent Task & QA — Design

Tanggal: 2026-11-03. Lanjutan Slice 1 (login + kanban). Semua teks UI dalam bahasa Inggris.

## Tujuan

Tiket Business harus jelas sebelum agent mengerjakan apa pun. Jadi membuat/mengedit tiket
memakai **modal ala Jira**, dan setiap kali tiket disimpan **Claude (`claude -p`) mereviu** apakah
tiketnya lengkap. Tiket baru boleh **Approved** setelah review lolos. Setelah Approved, Claude
membuat **Agent Task** dan **QA Test Case** yang kecil dan bisa dikerjakan.

Bukan tujuan (slice ini): menjalankan agent, live log, ERD/drawdb, multi-user, komentar dari user.
Anti-tujuan: Approved yang bisa ditembus tanpa review lolos; Claude dipanggil di tiap ketikan;
Agent Task sebesar "satu app".

## Slice (urutan pengerjaan, tiap slice bisa dipakai sendiri)

1. **Modal tiket.** Klik kartu atau "+ Add card" membuka modal. Kiri: Title, Description.
   Panel kanan: Status (read-only), Deadline (tanggal, opsional), tombol Approved.
   Menggantikan form kecil di kolom. Create dan edit memakai modal yang sama; setelah simpan
   pertama modal tetap terbuka dalam mode edit.
2. **Review Claude + gerbang Approved.** Setiap Save menjalankan review di background; hasilnya
   tampil sebagai komentar Claude di bawah Description.
3. **Business Flow (swimlanes).** Daftar swimlane di bawah Description; tambah/edit lewat modal
   editor.
4. **Agent Task + QA Test Case.** Dibuat Claude setelah Approved, tampil di bawah Business Flow,
   bisa diedit dengan modal yang sama gayanya.

## Review Claude (slice 2)

- Pemicu: tombol Save (bukan autosave). Review berjalan di server sebagai proses `claude -p`
  dan tidak memblokir modal; tiket menampilkan "Reviewing…" lalu hasilnya.
- Keluaran terstruktur: `{verdict: "pass" | "needs_work", summary, missing: [string]}`.
  `missing` ditampilkan sebagai daftar ("DoD belum jelas", "scope terlalu besar", …).
- Kriteria review: judul dan Description jelas; ada Definition of Done yang bisa diuji;
  scope kecil dan terkelola (satu perubahan terfokus, bukan seluruh aplikasi); mulai slice 3
  juga **minimal satu Business Flow**.
- Konteks: Claude boleh membaca folder board (read-only: Read/Grep/Glob lewat `--add-dir`) agar
  tahu produknya. Timeout 3 menit; gagal/timeout tampil "Review failed" dengan tombol Retry.
- **Gerbang Approved (ditegakkan di server, bukan hanya UI):** Approved hanya bisa diaktifkan bila
  review terbaru untuk isi saat ini berstatus `pass`. Mengubah Title, Description, atau
  Business Flow setelah Approved **membatalkan Approved** dan memicu review ulang. Selama review
  berjalan, tombol Approved nonaktif.

## Business Flow (slice 3)

- Setiap flow: nama + teks sumber. Tombol "+ Add flow" membuka modal: **editor teks di kiri,
  hasil gambar di kanan**, sintaks sama dengan `HEPHAESTUS/swim-lanes`
  (`A -> B: pesan`, `if/else/end`, `note`, dll.).
- Renderer swimlane di-port ke modul frontend One Piece (bukan iframe), sehingga diagram lama
  bisa disalin dan tetap terbaca. (Keputusanku, bukan dari percakapan.)

## Agent Task & QA (slice 4)

- Dipicu saat Approved. Claude menghasilkan daftar Agent Task (judul, deskripsi, urutan) dan QA
  Test Case (judul, langkah, hasil yang diharapkan).
- Pagar scope: tiap task = satu perubahan terfokus. Jika tiket terlalu besar untuk dipecah menjadi
  task kecil, Claude tidak membuat task melainkan menjawab "split this ticket" dengan usulan
  pemecahan, dan Approved dibatalkan.
- Task dan test case bisa diedit/ditambah/dihapus lewat modal bergaya sama dengan modal tiket.
  Menjalankan agent bukan bagian dari spec ini.

## Data (garis besar)

`cards` + `deadline` (tanggal, nullable) + `approved` (bool) + `content_version` (naik tiap
perubahan isi). Tabel baru: `card_reviews` (card_id, content_version, verdict, summary, missing,
status running/done/failed), `card_flows`, `card_tasks`, `card_qa`.
Perbandingan `content_version` review vs kartu menentukan "review basi" → Approved ditolak.

## Keputusan yang kuserahkan ke builder / keputusanku

- Approve tidak otomatis memindahkan kartu ke kolom Agent; pemindahan tetap drag manual.
- Hanya review terbaru yang ditampilkan; riwayat disimpan di tabel tapi belum ada UI-nya.
- Pemanggilan `claude -p` lewat satu fungsi tunggal agar mudah diganti/di-stub pada tes.
