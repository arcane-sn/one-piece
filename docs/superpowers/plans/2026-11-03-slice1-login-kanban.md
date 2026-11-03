# Slice 1: Login + Kanban Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Satu user bisa login, mengelola board (1 board = 1 product + daftar folder), dan memindahkan kartu di kanban Business → Agent → QA → Done.

**Architecture:** Binary Go (`net/http`) menyajikan JSON API di `/api/*` dan hasil build React di `/`. Data di SQLite. Frontend React + Vite memakai fetch ke API yang sama-origin (cookie sesi).

**Tech Stack:** Go ≥ 1.22 (pola `ServeMux` dengan method), `modernc.org/sqlite`, `golang.org/x/crypto/bcrypt`, `golang.org/x/term`; React + Vite + `react-router-dom`.

**Spec:** `docs/superpowers/specs/2026-11-03-slice1-login-kanban-design.md`

## Global Constraints

- Stack: React (frontend) + Go (backend), SQLite pure-Go tanpa cgo (`modernc.org/sqlite`).
- Satu user saja; tanpa halaman registrasi; akun dibuat lewat CLI.
- Password bcrypt; sesi cookie `op_session`, `HttpOnly`, `SameSite=Lax`, `Path=/`.
- Server hanya listen di `127.0.0.1` (default `127.0.0.1:5300`; Vite dev `127.0.0.1:5301`, proxy `/api` ke 5300).
- Folder board: path absolut yang ada di disk (direktori), divalidasi saat disimpan.
- Kolom tetap, nilai `status`: `business`, `agent`, `qa`, `done` (label UI: Business, Agent, QA, Done).
- Hapus board menghapus kartunya, UI meminta konfirmasi.
- Layout: `backend/`, `frontend/`, `docs/`, satu `start.sh`. Repo public: jangan commit `.env`, `*.db`, password.
- Test otomatis hanya untuk API (Go `testing` + `httptest`, DB sementara di `t.TempDir()`); frontend diverifikasi manual.
- Bukan bagian slice ini: review tiket, tiket Agent/QA, `claude -p`, live log, swim-lanes, drawdb.

## Review Focus

1. Path folder relatif / tidak ada / bukan direktori / duplikat / pakai trailing slash → ditolak atau dinormalisasi (Task 3).
2. Nama board atau judul kartu kosong, hanya spasi, atau terlalu panjang → 400 (Task 3, 4).
3. Pindah kartu ke `status` tak dikenal, atau `position` negatif / di luar rentang → 400 / dijepit ke rentang valid (Task 4).
4. Cookie sesi kedaluwarsa atau palsu, dan POST/PUT/DELETE tanpa `Content-Type: application/json` (pertahanan CSRF murah) → 401 / 415 (Task 2).
5. Alamat listen non-loopback (`0.0.0.0:5300`) → server menolak start (Task 5).

---

## File Structure

```
backend/go.mod
backend/cmd/server/main.go            serve + subcommand create-user, guard loopback
backend/cmd/server/main_test.go
backend/internal/store/store.go       Open, migrasi skema
backend/internal/store/users.go       user + sesi
backend/internal/store/boards.go      board + validasi folder
backend/internal/store/cards.go       kartu + move
backend/internal/api/api.go           New(), router, middleware, helper JSON, static + SPA fallback
backend/internal/api/auth.go          /api/login /logout /me
backend/internal/api/boards.go
backend/internal/api/cards.go
backend/internal/api/*_test.go
frontend/                             Vite React app
start.sh
README.md
```

### Task 1: Store + skema

**Files:**
- Create: `backend/go.mod` (module `github.com/arcane-sn/one-piece/backend`), `backend/internal/store/store.go`
- Test: `backend/internal/store/store_test.go`

**Interfaces:**
- Produces: `type Store struct{ DB *sql.DB }`, `func Open(path string) (*Store, error)` (buat file jika belum ada, `PRAGMA foreign_keys=ON`, jalankan migrasi idempoten). Error sentinel: `var ErrNotFound = errors.New("not found")`, `type ValidationError struct{ Msg string }` (implements `error`).
- Skema: `users(id, username UNIQUE, password_hash)`, `sessions(token PK, user_id, expires_at INTEGER unix)`, `boards(id, name)`, `board_folders(id, board_id → boards ON DELETE CASCADE, path, position)`, `cards(id, board_id → boards ON DELETE CASCADE, title, description, status, position)`.

- [ ] **Step 1: Tulis test `TestOpenIsIdempotent`** — `Open` dua kali pada path yang sama tanpa error.
- [ ] **Step 2: Tulis test `TestDeletingBoardCascades`** — insert board + 1 folder + 1 kartu lewat `DB.Exec`, hapus board, assert `COUNT(*)` di `board_folders` dan `cards` = 0.
- [ ] **Step 3: Jalankan** `cd backend && go test ./internal/store/...` → FAIL (belum ada `Open`).
- [ ] **Step 4: Implement `Open`** dan skema di atas (`CREATE TABLE IF NOT EXISTS`); `go get modernc.org/sqlite`.
- [ ] **Step 5: Jalankan test** → PASS.
- [ ] **Step 6: Commit** `feat(store): sqlite schema`.

### Task 2: User, sesi, auth API

**Files:**
- Create: `backend/internal/store/users.go`, `backend/internal/api/api.go`, `backend/internal/api/auth.go`
- Test: `backend/internal/store/users_test.go`, `backend/internal/api/auth_test.go`

**Interfaces:**
- Consumes: `store.Open`, `ErrNotFound`, `ValidationError`.
- Produces (store): `func (s *Store) CreateUser(username, password string) error` (username di-trim & tidak kosong, password ≥ 8 karakter, bcrypt; user kedua ditolak dengan `ValidationError` — satu user saja), `func (s *Store) CheckPassword(username, password string) (userID int64, ok bool)`, `func (s *Store) NewSession(userID int64) (token string, err error)` (32 byte acak hex, berlaku 30 hari), `func (s *Store) SessionUser(token string) (username string, ok bool)` (false jika kedaluwarsa), `func (s *Store) DeleteSession(token string) error`.
- Produces (api): `func New(s *store.Store, staticDir string) http.Handler`. Semua `/api/*` kecuali `POST /api/login` lewat middleware `requireAuth` (401 `{"error":"unauthorized"}`). Method POST/PUT/DELETE tanpa `Content-Type: application/json` → 415. Body dibatasi 1 MiB (`http.MaxBytesReader`). Helper `writeJSON(w, status, v)` dan `writeErr(w, err)` yang memetakan `ErrNotFound`→404, `ValidationError`→400 `{"error":msg}`.
- Endpoint: `POST /api/login {username,password}` → 204 + cookie; salah → 401 dengan pesan sama untuk user tak ada / password salah, dan `time.Sleep(300ms)` sebelum membalas. `POST /api/logout` → 204 + hapus sesi & cookie. `GET /api/me` → `{"username":"..."}`.

- [ ] **Step 1: Tulis test store** `TestCreateUserRejectsShortPassword`, `TestSecondUserRejected`, `TestCheckPassword` (benar → ok, salah → !ok), `TestSessionExpires` (set `expires_at` ke masa lalu lewat `DB.Exec`, `SessionUser` → false).
- [ ] **Step 2: Tulis test API** (helper `newTestServer(t)` yang membuat store + user `owner`/`password123`): `TestLoginSetsCookie` (204, cookie `op_session` HttpOnly, SameSite Lax), `TestLoginWrongPasswordSameAsUnknownUser` (keduanya 401, body identik), `TestBoardsRequireAuth` (`GET /api/boards` tanpa cookie → 401, dengan cookie palsu → 401), `TestMutationNeedsJSONContentType` (POST login dengan `text/plain` → 415), `TestLogoutInvalidatesSession`.
- [ ] **Step 3: Jalankan** `go test ./...` → FAIL.
- [ ] **Step 4: Implement** fungsi store dan handler di atas (`golang.org/x/crypto/bcrypt`, `crypto/rand`).
- [ ] **Step 5: Jalankan** `go test ./...` → PASS.
- [ ] **Step 6: Commit** `feat(api): login, session, auth middleware`.

### Task 3: Board + validasi folder

**Files:**
- Create: `backend/internal/store/boards.go`, `backend/internal/api/boards.go`
- Test: `backend/internal/store/boards_test.go`, `backend/internal/api/boards_test.go`

**Interfaces:**
- Consumes: `store.Open`, `ErrNotFound`, `ValidationError`, API `New`/`requireAuth`/`writeJSON`/`writeErr`.
- Produces (store): `type Board struct{ ID int64 \`json:"id"\`; Name string \`json:"name"\`; Folders []string \`json:"folders"\` }`; `func (s *Store) CreateBoard(name string, folders []string) (Board, error)`, `ListBoards() ([]Board, error)`, `GetBoard(id int64) (Board, error)`, `UpdateBoard(id int64, name string, folders []string) (Board, error)`, `DeleteBoard(id int64) error`.
- Validasi: nama di-trim, 1–100 karakter. Folder: di-trim, `filepath.Clean`, harus `filepath.IsAbs`, harus ada dan berupa direktori (`os.Stat`), duplikat setelah Clean dibuang dengan urutan awal dipertahankan. Nol folder diperbolehkan (keputusan saya).
- Produces (api): `GET /api/boards` → `[]Board` (array kosong, bukan `null`), `POST /api/boards` → 201 `Board`, `GET /api/boards/{id}`, `PUT /api/boards/{id}`, `DELETE /api/boards/{id}` → 204. Body `{"name":"...","folders":["/abs/path"]}`. `{id}` non-angka → 404.

- [ ] **Step 1: Tulis test store** `TestFolderRejectsRelativePath`, `TestFolderRejectsMissingDir`, `TestFolderRejectsFile` (path ke file biasa), `TestFolderNormalizesAndDedupes` (`dir`, `dir/`, `dir/../dir` → satu entri `dir`), `TestBoardNameRejectsBlankAndTooLong` (`"   "`, 101 karakter), `TestDeleteBoardRemovesCards` (buat kartu lewat `DB.Exec`, hapus, kartu 0).
- [ ] **Step 2: Tulis test API** `TestBoardCRUD` (create→list→update→get→delete), `TestListBoardsEmptyIsArray` (body `[]`), `TestBoardBadIDIs404` (`/api/boards/abc`, `/api/boards/9999`), `TestBoardBadFolderIs400`.
- [ ] **Step 3: Jalankan** → FAIL.
- [ ] **Step 4: Implement**; update board mengganti seluruh daftar folder dalam satu transaksi.
- [ ] **Step 5: Jalankan** `go test ./...` → PASS.
- [ ] **Step 6: Commit** `feat: boards with validated folders`.

### Task 4: Kartu + move

**Files:**
- Create: `backend/internal/store/cards.go`, `backend/internal/api/cards.go`
- Test: `backend/internal/store/cards_test.go`, `backend/internal/api/cards_test.go`

**Interfaces:**
- Consumes: `Store.GetBoard`, `ErrNotFound`, `ValidationError`.
- Produces (store): `type Card struct{ ID, BoardID int64; Title, Description, Status string; Position int }` (json: `id, boardId, title, description, status, position`); `var Statuses = []string{"business","agent","qa","done"}`; `func (s *Store) CreateCard(boardID int64, title, description string) (Card, error)` (status `business`, ditaruh di akhir kolom), `ListCards(boardID int64) ([]Card, error)` (urut status sesuai `Statuses`, lalu position), `UpdateCard(id int64, title, description string) (Card, error)`, `MoveCard(id int64, status string, position int) (Card, error)`, `DeleteCard(id int64) error`.
- Aturan: judul di-trim, 1–200 karakter; deskripsi ≤ 10000. `MoveCard` dalam satu transaksi: cabut kartu dari kolom asal, sisipkan di `position` kolom tujuan (dijepit ke `[0, len]`), lalu renumber kolom asal dan tujuan menjadi 0..n-1 tanpa celah.
- Produces (api): `GET /api/boards/{id}/cards`, `POST /api/boards/{id}/cards {title,description}` → 201, `PUT /api/cards/{id}`, `POST /api/cards/{id}/move {status,position}`, `DELETE /api/cards/{id}` → 204. Board tak ada → 404.

- [ ] **Step 1: Tulis test store** `TestCreateCardStartsInBusinessAtEnd`, `TestCardTitleRejectsBlankAndTooLong`, `TestMoveAcrossColumnsKeepsPositionsContiguous` (3 kartu di business, pindah yang tengah ke `agent` posisi 0; business = [0,1], agent = [0]), `TestMoveWithinColumnReorders`, `TestMovePositionClamped` (`-5` → 0, `999` → akhir), `TestMoveUnknownStatusRejected`, `TestCreateCardOnMissingBoardIsNotFound`.
- [ ] **Step 2: Tulis test API** `TestCardFlow` (create → move ke `done` → list menunjukkan `done`), `TestMoveBadStatusIs400`, `TestCardOnMissingBoardIs404`.
- [ ] **Step 3: Jalankan** → FAIL.
- [ ] **Step 4: Implement**.
- [ ] **Step 5: Jalankan** `go test ./...` → PASS.
- [ ] **Step 6: Commit** `feat: cards and move`.

### Task 5: Server main, static, guard loopback

**Files:**
- Create: `backend/cmd/server/main.go`
- Modify: `backend/internal/api/api.go` (static + SPA fallback)
- Test: `backend/cmd/server/main_test.go`, `backend/internal/api/static_test.go`

**Interfaces:**
- Consumes: `store.Open`, `Store.CreateUser`, `api.New`.
- Produces: `func ListenAddrOK(addr string) bool` (true hanya jika host `127.0.0.1`, `localhost`, atau `::1`). CLI: `server [-addr 127.0.0.1:5300] [-db one-piece.db] [-static ../frontend/dist]` untuk serve; `server create-user <username>` membaca password dari stdin tanpa echo (`golang.org/x/term`; jika stdin bukan terminal, baca satu baris) lalu memanggil `CreateUser`. Alamat non-loopback → exit 1 dengan pesan jelas.
- Static: file ada di `staticDir` → disajikan; path bukan `/api/` yang tidak ada → `index.html`; `/api/` yang tidak ada → 404 JSON.

- [ ] **Step 1: Tulis test** `TestListenAddrOK` (`127.0.0.1:5300`, `localhost:1`, `[::1]:1` → true; `0.0.0.0:5300`, `:5300`, `192.168.1.5:80` → false), `TestSPAFallback` (temp dir berisi `index.html`; `GET /boards/1` → isi index; `GET /api/nope` dengan sesi valid → 404 JSON).
- [ ] **Step 2: Jalankan** → FAIL.
- [ ] **Step 3: Implement** `ListenAddrOK`, static handler, `main`.
- [ ] **Step 4: Jalankan** `go test ./...` → PASS.
- [ ] **Step 5: Verifikasi manual** — `go run ./cmd/server create-user owner`, lalu `go run ./cmd/server` dan `curl -i -X POST localhost:5300/api/login -H 'Content-Type: application/json' -d '{"username":"owner","password":"..."}'` → 204 + `Set-Cookie`.
- [ ] **Step 6: Commit** `feat: server entrypoint, static, loopback guard`.

### Task 6: Frontend scaffold + login + auth gate

**Files:**
- Create: `frontend/` (Vite React: `npm create vite@latest frontend -- --template react`), `frontend/src/api.js`, `frontend/src/App.jsx`, `frontend/src/pages/Login.jsx`; `frontend/vite.config.js` (port 5301, host `127.0.0.1`, proxy `/api` → `http://127.0.0.1:5300`)

**Interfaces:**
- Produces: `api(path: string, opts?: {method?: string, body?: object}): Promise<any>` — fetch JSON, header `Content-Type: application/json` pada body, mengembalikan `null` untuk 204, melempar `Error` dengan `.status` dan `.message` dari `{"error"}`. `App` memanggil `GET /api/me`; 401 → render `Login`; sukses → router (`/` Dashboard, `/boards/:id` Board) dengan tombol Logout.

- [ ] **Step 1: Scaffold**, `npm i react-router-dom`, atur proxy.
- [ ] **Step 2: Implement** `api.js`, `Login.jsx` (username, password, pesan error), `App.jsx` (gate + route kosong sementara).
- [ ] **Step 3: Verifikasi manual** — jalankan backend + `npm run dev`; buka `http://127.0.0.1:5301`: login salah menampilkan error, login benar masuk, refresh tetap masuk, Logout kembali ke login.
- [ ] **Step 4: Commit** `feat(frontend): scaffold, login, auth gate`.

### Task 7: Dashboard + kelola board

**Files:**
- Create: `frontend/src/pages/Dashboard.jsx`, `frontend/src/components/BoardForm.jsx`

**Interfaces:**
- Consumes: `api`, `GET/POST/PUT/DELETE /api/boards`.
- Produces: `Dashboard` menampilkan daftar board (klik → `/boards/:id`), tombol tambah/ubah/hapus lewat `BoardForm` (props: `initial?: Board`, `onSaved(board)`, `onCancel()`; nama + textarea folder satu path per baris; pesan error 400 dari server ditampilkan apa adanya), konfirmasi `window.confirm` sebelum hapus, dan bagian "Sedang dikerjakan Agent" berisi teks "Belum ada Agent yang berjalan".

- [ ] **Step 1: Implement** halaman dan form.
- [ ] **Step 2: Verifikasi manual** — buat board dengan folder valid (berhasil), folder relatif dan folder tak ada (error tampil), ubah, hapus dengan konfirmasi (Batal tidak menghapus).
- [ ] **Step 3: Commit** `feat(frontend): dashboard and board management`.

### Task 8: Kanban board

**Files:**
- Create: `frontend/src/pages/Board.jsx`, `frontend/src/components/Column.jsx`, `frontend/src/components/CardItem.jsx`

**Interfaces:**
- Consumes: `api`, `GET /api/boards/{id}`, cards API.
- Produces: `Board` memuat board + kartu, merender 4 kolom (Business, Agent, QA, Done). Form tambah kartu (judul, deskripsi) hanya di kolom Business. Kartu: klik untuk ubah, tombol hapus. Drag-and-drop HTML5 native (tanpa library): drop pada kartu menyisipkan sebelum kartu itu, drop pada kolom kosong/area bawah menaruh di akhir; UI optimistik, panggil `POST /api/cards/{id}/move`, dan jika gagal muat ulang daftar kartu. Board tak ada (404) → pesan "Board tidak ditemukan" + link ke dashboard. Daftar folder board ditampilkan di header.

- [ ] **Step 1: Implement** halaman, kolom, kartu, dan logika drop (hitung index tujuan dari kartu target).
- [ ] **Step 2: Verifikasi manual** — tambah 3 kartu; geser antar kolom dan urutan dalam kolom; refresh, urutan tetap; hapus kartu; buka `/boards/9999` → pesan tidak ditemukan.
- [ ] **Step 3: Commit** `feat(frontend): kanban board`.

### Task 9: start.sh, README, cek end-to-end

**Files:**
- Create: `start.sh`, `README.md`
- Modify: `.gitignore` (tambah `frontend/dist/`)

**Interfaces:**
- Consumes: semua task sebelumnya.
- Produces: `./start.sh` → `npm ci && npm run build` di `frontend/` bila `dist` belum ada, lalu `go run ./cmd/server -static ../frontend/dist` di `backend/`, mencetak `http://127.0.0.1:5300`. README: langkah setup (buat user, jalankan), peringatan repo public (jangan commit password/db), daftar yang belum ada (non-goals).

- [ ] **Step 1: Tulis** `start.sh` (`chmod +x`) dan README.
- [ ] **Step 2: Verifikasi end-to-end** dari state bersih (hapus `one-piece.db` dan `frontend/dist`): create-user → `./start.sh` → login → buat board → tambah & geser kartu → logout. Pastikan `curl http://<IP-LAN>:5300` ditolak (tidak listen di luar loopback) dan `go test ./...` hijau.
- [ ] **Step 3: Commit** `chore: start script and readme`, lalu `git push`.
