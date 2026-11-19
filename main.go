package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bell-budut/audio"
	"bell-budut/database"
	"bell-budut/scheduler"
	"bell-budut/updater"
	"bell-budut/version"
	"bell-budut/web"
)

func main() {
	port := flag.Int("port", 8080, "Port server HTTP")
	dbPath := flag.String("db", filepath.Join("data", "bell.db"), "Path file database SQLite")
	audioDir := flag.String("audio", filepath.Join("data", "audio"), "Path direktori penyimpanan file audio")
	flag.Parse()

	fmt.Println("==================================================================")
	fmt.Println("          SISTEM BEL SEKOLAH OTOMATIS & TERJADWAL                ")
	fmt.Printf("                    BELL BUDUT v%s\n", version.Version)
	fmt.Println("==================================================================")

	// Remove leftovers of a previous OTA update (old exe)
	updater.CleanupOld()

	// Ensure directories exist
	if err := os.MkdirAll(*audioDir, 0755); err != nil {
		log.Fatalf("Gagal membuat direktori audio: %v", err)
	}

	// 1. Initialize SQLite Database
	fmt.Printf("[1/4] Menginisialisasi database SQLite di: %s ...\n", *dbPath)
	db, err := database.InitDB(*dbPath)
	if err != nil {
		log.Fatalf("Fatal: Gagal menginisialisasi database: %v", err)
	}
	fmt.Println("      ✓ Database siap (Grup bawaan, sampel nada, dan pengaturan aktif)")

	// 2. Initialize Audio Manager
	fmt.Printf("[2/4] Menginisialisasi sistem audio server di: %s ...\n", *audioDir)
	audioMgr := audio.NewManager(*audioDir)
	fmt.Println("      ✓ Audio Manager siap (Dukungan Windows Soundcard & Hybrid Web Audio)")

	// 3. Initialize & Start Scheduler
	fmt.Println("[3/4] Menjalankan scheduler bel otomatis & broadcast SSE ...")
	sched := scheduler.NewScheduler(db, audioMgr, *audioDir)
	sched.Start()
	defer sched.Stop()
	fmt.Println("      ✓ Scheduler aktif (Pengecekan presisi 1 detik)")

	// 4. Initialize Web Server
	fmt.Println("[4/4] Menyiapkan server web UI berbasis Tailwind CSS ...")
	server := web.NewServer(db, audioMgr, sched, *audioDir)
	server.SetPort(*port)
	handler := server.Routes()

	addr := fmt.Sprintf(":%d", *port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	// Print Network IP Addresses for easy Wi-Fi access by school staff
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("Aplikasi Bel Sekolah berhasil berjalan!\n")
	fmt.Printf("• Akses di komputer ini : http://localhost:%d\n", *port)
	fmt.Printf("• Layar Kiosk / TV       : http://localhost:%d/kiosk\n", *port)

	localIPs := getLocalIPAddresses()
	if len(localIPs) > 0 {
		fmt.Printf("• Akses dari HP/Laptop via Wi-Fi Sekolah:\n")
		for _, ip := range localIPs {
			fmt.Printf("  -> http://%s:%d\n", ip, *port)
		}
	}
	fmt.Println("------------------------------------------------------------------")
	fmt.Println("PIN Default Admin: 1234 (dapat diubah di menu Pengaturan)")
	fmt.Println("Tekan Ctrl+C di terminal ini untuk mematikan aplikasi.")
	fmt.Println("==================================================================")

	// Start server in background.
	// After an OTA update the previous process may still hold the port for a moment,
	// so keep retrying the bind for up to 30 seconds.
	go func() {
		var ln net.Listener
		var err error
		for attempt := 0; attempt < 30; attempt++ {
			ln, err = net.Listen("tcp", addr)
			if err == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM, or after an OTA update
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	server.SetRestartFunc(func() {
		fmt.Println("\nUpdate terpasang, menjalankan versi baru...")
		if err := updater.Restart(); err != nil {
			log.Printf("Gagal menjalankan versi baru: %v", err)
			return
		}
		select {
		case stopChan <- os.Interrupt:
		default:
		}
	})

	<-stopChan

	fmt.Println("\nMematikan aplikasi bel sekolah dengan aman...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = audioMgr.Stop()
	sched.Stop()
	_ = httpServer.Shutdown(ctx)
	fmt.Println("Aplikasi berhasil ditutup. Sampai jumpa!")
}

func getLocalIPAddresses() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}

	for _, a := range addrs {
		if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				ips = append(ips, ipNet.IP.String())
			}
		}
	}
	return ips
}
