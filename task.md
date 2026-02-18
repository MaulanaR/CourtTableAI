# Tasks: Implementation Roadmap

## Phase 1: Environment & Database Setup
- [ ] Inisialisasi proyek: `go mod init go-debate`.
- [ ] Desain skema Database (SQLite):
    - Table `agents`: `id, name, provider_url, api_token, model_name, timeout_seconds`.
    - Table `discussions`: `id, topic, final_summary, created_at`.
    - Table `discussion_logs`: `id, discussion_id, agent_id, content, status (success/timeout)`.

## Phase 2: Master Data Management (CRUD)
- [ ] Buat API Endpoint untuk Create/Read/Update/Delete Agent.
- [ ] Buat UI Form untuk input setting Agent (URL & Token).
- [ ] Implementasi fungsi `Ping` untuk cek koneksi AI Agent.

## Phase 3: Core Orchestrator (The Debate Engine)
- [ ] Implementasi `Agent Client` menggunakan `http.Client` dengan `context.WithTimeout`.
- [ ] Buat fungsi `RunDebate`:
    - Loop melalui daftar ID Agent yang dipilih.
    - Kirim prompt awal + history chat sebelumnya ke agen aktif.
    - Handling error jika respon melebihi durasi timeout.
- [ ] Implementasi logika "Debat": Agen berikutnya harus diberitahu untuk "merespon/mendebat" jawaban agen sebelumnya.

## Phase 4: Discussion UI & Real-time Updates
- [ ] Buat UI "Discussion Room".
- [ ] Implementasi Webhook atau Server-Sent Events (SSE) agar hasil debat muncul secara real-time di layar tanpa refresh.
- [ ] Tambahkan indikator loading/status untuk agen yang sedang berpikir.

## Phase 5: History & Reporting
- [ ] Buat halaman History yang menampilkan daftar diskusi lama.
- [ ] Buat detail view untuk melihat alur debat (siapa bicara apa).