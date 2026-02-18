# Planning: Multi-Agent Debate System (Go-Debate)

## Project Overview
Sebuah aplikasi berbasis web untuk mengelola agen AI dan memfasilitasi diskusi/debat antar agen secara otomatis menggunakan skema "Round Robin with Timeout" untuk menghasilkan jawaban terbaik bagi pengguna.

## Tech Stack
- **Backend:** Go (Golang) dengan framework Echo atau Fiber.
- **Frontend:** HTML5/HTMX, CSS (Tailwind CSS), dan JavaScript (Vanilla/HTMX).
- **Database:** SQLite (untuk Master Data & History karena ringan dan lokal).
- **LLM Integration:** REST API (mendukung OpenAI format atau Ollama API).

## System Architecture
1. **Orchestrator:** Logika utama yang mengatur giliran bicara AI, mengelola timeout, dan menyusun riwayat debat.
2. **Agent Interface:** Adapter untuk menghubungkan berbagai penyedia AI (Ollama, OpenAI, Claude).
3. **Frontend Dashboard:** UI untuk manajemen master data dan ruang diskusi.

## Workflow Diskusi
1. User memasukkan topik & memilih daftar AI.
2. Sistem mengirim prompt ke AI #1.
3. Jawaban AI #1 dikirim ke AI #2 sebagai konteks untuk dikritik/ditanggapi.
4. Jika AI #n melewati batas timeout, sistem memberikan flag "Skipped" dan lanjut ke agen berikutnya.
5. Setelah semua agen bicara atau limit putaran habis, sistem merangkum hasil akhir.