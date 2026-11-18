# 🔔 Sistem Bel Sekolah Otomatis - Bell Budut

Aplikasi manajemen bel sekolah otomatis berbasis **Golang Murni** (tanpa CGO, standalone single binary) dengan **Web UI responsif berbasis Tailwind CSS**.

---

## 🚀 Cara Menjalankan & Auto-Start saat Komputer Dinyalakan

### 1. Auto-Start Otomatis saat Komputer Dinyalakan (Rekomendasi Utama)
Aplikasi sudah didaftarkan ke Windows Startup! Begitu komputer sekolah dinyalakan di pagi hari dan masuk ke Windows:
- Aplikasi bel otomatis berjalan di **latar belakang (background)** tanpa perlu ada yang membuka atau mengklik apa pun.
- Tidak ada jendela CMD hitam yang mengganggu.
- Anda bisa mengaktifkan/menonaktifkan auto-start ini dengan dua cara:
  * **Cara 1 (Lewat Web):** Buka menu **Pengaturan** di dashboard web -> ada saklar centang **"Jalankan Otomatis saat Komputer Dinyalakan (Auto-Start Windows)"**.
  * **Cara 2 (Lewat File Bat):** Cukup klik dua kali `Pasang_AutoStart_Windows.bat` (atau `Hapus_AutoStart_Windows.bat` jika ingin mematikan).

### 2. Menjalankan Manual
Jika auto-start dimatikan, Anda tetap bisa menjalankannya kapan saja dengan mengklik dua kali:
- **`Jalankan_Bel.bat`** (Membuka server sekaligus membuka browser otomatis)
- Atau **`Jalankan_Background.vbs`** (Menjalankan diam-diam di background)

### 3. Alamat Akses Web
- **Dashboard Admin & Operator:** [http://localhost:8080](http://localhost:8080)
- **Layar Kiosk / TV Display:** [http://localhost:8080/kiosk](http://localhost:8080/kiosk)

### Akses dari HP / Laptop lain via Wi-Fi Sekolah
Buka browser di HP/Laptop yang terhubung ke Wi-Fi yang sama, lalu masukkan alamat IP komputer server:
`http://<IP-KOMPUTER-SERVER>:8080`

---

## 🔑 Keamanan & PIN Admin
- **PIN Default:** `1234`
- Siapapun dapat melihat jam digital dan jadwal bel (tampilan publik).
- Aksi krusial (menambah/mengedit jadwal, mengubah grup, upload audio, import Excel, dan backup database) dilindungi oleh PIN keamanan.
- PIN dapat diubah kapan saja di menu **Pengaturan**.

---

## 🌟 Fitur Utama

### 1. Hybrid Audio Playback
- **Speaker Server:** Komputer server dicolok via kabel audio jack 3.5mm ke amplifier / sound system sekolah (TOA). Suara diputar langsung lewat hardware soundcard Windows (MCI).
- **Web Audio (Browser):** Disediakan toggle *Suara Browser: Aktif/Nonaktif*. Memungkinkan laptop guru/operator membunyikan bel lewat speaker browser saat tab dibuka.

### 2. Sistem Grup Jadwal & Rentang Tanggal
- Mendukung banyak profil/grup jadwal:
  - **Grup Reguler (Senin - Jumat):** Jam belajar normal harian.
  - **Grup Ujian (PTS / PAS):** Jam khusus saat pekan ujian.
  - **Grup Ramadhan:** Jam khusus yang dipadatkan selama bulan puasa.
- **Dua Cara Aktivasi Grup:**
  1. **Manual Toggle:** Klik *"Aktifkan Sekarang"* kapan pun ingin berganti grup.
  2. **Rentang Tanggal Otomatis:** Setel tanggal mulai dan tanggal selesai (contoh: aktif otomatis tanggal 1 Des s/d 10 Des). Sistem akan memprioritaskan grup tersebut selama rentang tanggal, lalu otomatis kembali ke grup default setelah selesai.

### 3. Integrasi Excel (.xlsx) Pintar & Dropdown Otomatis
- **Pilihan Nama Audio Langsung (Dropdown Excel):**
  * Di template Excel (Kolom E), pengguna tinggal memilih nama lagu/kegiatannya dari dropdown tanpa perlu mencatat atau menghafal kode audio.
  * Kolom F secara otomatis mengisi Kode Audio (`AUD-XX`) melalui rumus Excel `=VLOOKUP()`.
- **Ekspor per Grup:** Anda dapat mengekspor satu grup jadwal tertentu saja atau seluruh grup gabungan.
- **Import Fleksibel (Popup Pintar):**
  * Opsi **Perbarui Grup yang Ada** (dengan pilihan *Timpa Total* atau *Tambahkan Saja*).
  * Opsi **Buat Grup Baru Otomatis** (cukup ketik nama grup baru di aplikasi).
  * Opsi **Ikuti Kolom File Excel**.
- **Template Selalu Terupdate:** Setiap ada lagu baru yang diunggah, template Excel yang diunduh otomatis memuat lagu baru tersebut.

### 4. Upload Audio Massal (Batch Upload)
- Mendukung unggah banyak file lagu MP3/WAV sekaligus dalam 1 kali jalan via file explorer (`Ctrl+A` / `Shift+Click`) atau fitur Drag & Drop.
- Judul lagu otomatis disesuaikan dari nama file lagu dan diberikan kode urut `AUD-XX` secara otomatis.

### 5. Pemisahan Suara (Audio Isolator)
- Panduan dan alat cepat 1-klik untuk mengatur Volume Mixer Windows agar suara notifikasi WhatsApp, video YouTube, atau suara sistem komputer lainnya **hanya keluar di speaker monitor/headset komputer**, sementara **nada bel sekolah tetap keluar bersih ke speaker utama (TOA) sekolah**.

### 6. Cadangkan & Pulihkan Database (Backup & Restore)
- **Backup:** Satu klik untuk mengunduh snapshot database `bell.db`.
- **Restore:** Unggah file cadangan `.db` kapan pun dibutuhkan. File saat ini akan otomatis di-backup terlebih dahulu demi keamanan.

### 5. Mode Libur & Tombol Darurat
- **Mode Libur:** Saklar cepat untuk mematikan semua jadwal bel otomatis saat tanggal merah / libur semester tanpa perlu menghapus jadwal.
- **Aksi Cepat:**
  - 🔔 *1x Tit (Bip)*
  - 🔔 *2 Nada (Ding-Dong)*
  - 🔔 *3 Nada (Do-Mi-Sol / Istirahat)*
  - 🔔 *4 Nada Westminster (Masuk / Pulang)*
  - 🚨 *Sirine Darurat / Evakuasi* (dengan konfirmasi keamanan).

### 6. Layar Kiosk / TV Fullscreen (`/kiosk`)
- Tampilan jam digital raksasa, hitung mundur (countdown) ke bel berikutnya, dan tabel daftar jadwal hari ini.
- Cocok dipasang di TV ruang guru, lobi sekolah, atau ruang piket.

---

## 📁 Struktur Direktori
```
F:\1. Projek\Bell Budut\
├── bell-budut.exe          // Binary executable utama
├── Jalankan_Bel.bat        // Shortcut klik 2x untuk menjalankan
├── main.go                 // Kode sumber entrypoint
├── database/               // SQLite pure Go & skema tabel
├── audio/                  // Windows soundcard MCI player
├── scheduler/              // Loop presisi 1 detik & broadcast SSE
├── excel/                  // Generator template & importer .xlsx
├── web/
│   ├── handler.go          // REST API & route handlers
│   ├── static/
│   │   ├── css/tailwind.min.css // Tailwind CSS offline
│   │   └── js/app.js       // Logika interaktif frontend
│   └── templates/
│       ├── index.html      // Dashboard utama
│       └── kiosk.html      // Tampilan layar penuh Kiosk/TV
└── data/
    ├── bell.db             // Database SQLite (auto-generated)
    └── audio/              // Tempat penyimpanan file WAV/MP3
```

