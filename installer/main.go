//go:build windows

// Command installer is a one-file Setup.exe for Bell Budut. It contains the app
// plus the school's current data (schedules, settings, audio) in payload.zip and:
//   - extracts everything to C:\BellBudut (or a folder of choice)
//   - keeps existing data when re-installing over an old installation (optional)
//   - creates desktop shortcuts, enables Windows auto-start, opens the firewall port
//   - starts the app in the background and opens the dashboard
//
// Build with Buat_Installer.bat (it creates payload.zip first).
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bell-budut/version"
)

//go:embed payload.zip
var payload []byte

const defaultDir = `C:\BellBudut`

var in = bufio.NewReader(os.Stdin)

func main() {
	fmt.Println("==================================================================")
	fmt.Printf("     INSTALLER SISTEM BEL SEKOLAH BUDUT  -  versi %s\n", version.Version)
	fmt.Println("==================================================================")
	fmt.Println("Installer ini berisi aplikasi + jadwal, pengaturan, dan audio yang")
	fmt.Println("sudah disiapkan. Setelah selesai, bel langsung siap dipakai.")
	fmt.Println()

	if err := run(); err != nil {
		fmt.Println()
		fmt.Println("[GAGAL]", err)
	}
	fmt.Println()
	ask("Tekan Enter untuk menutup jendela ini...")
}

func run() error {
	dir := ask(fmt.Sprintf("Folder instalasi [%s]: ", defaultDir))
	if dir == "" {
		dir = defaultDir
	}
	dir = filepath.Clean(dir)

	// Stop a running copy so files can be replaced
	if isRunning() {
		if !yes("Aplikasi bel sedang berjalan. Hentikan dulu untuk melanjutkan? [Y/n]: ", true) {
			return fmt.Errorf("instalasi dibatalkan")
		}
		_ = exec.Command("taskkill", "/IM", "bell-budut.exe", "/F").Run()
		time.Sleep(2 * time.Second)
	}

	keepData := false
	if _, err := os.Stat(filepath.Join(dir, "data", "bell.db")); err == nil {
		fmt.Println()
		fmt.Println("Ditemukan data lama (jadwal & pengaturan) di folder ini.")
		keepData = yes("Pertahankan data lama? (n = timpa dengan data dari installer) [Y/n]: ", true)
	}

	fmt.Println()
	fmt.Println("[1/5] Menyalin file aplikasi ke", dir, "...")
	n, err := extract(dir, keepData)
	if err != nil {
		return err
	}
	fmt.Printf("      ✓ %d file terpasang\n", n)

	fmt.Println("[2/5] Membuat shortcut di Desktop ...")
	if err := makeShortcuts(dir); err != nil {
		fmt.Println("      ! Shortcut gagal dibuat:", err)
	} else {
		fmt.Println("      ✓ Shortcut 'Bel Sekolah' & 'Bel Sekolah - Layar Kiosk'")
	}

	fmt.Println("[3/5] Mengaktifkan auto-start saat Windows menyala ...")
	vbs := filepath.Join(dir, "Jalankan_Background.vbs")
	if err := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "BellBudut", "/t", "REG_SZ", "/d", fmt.Sprintf(`wscript.exe "%s"`, vbs), "/f").Run(); err != nil {
		fmt.Println("      ! Gagal mengatur auto-start:", err)
	} else {
		fmt.Println("      ✓ Bel otomatis berjalan setiap komputer dinyalakan")
	}

	fmt.Println("[4/5] Membuka akses dari HP/Laptop lain (firewall port 8080) ...")
	exe := filepath.Join(dir, "bell-budut.exe")
	_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name=Bell Budut").Run()
	if err := exec.Command("netsh", "advfirewall", "firewall", "add", "rule", "name=Bell Budut",
		"dir=in", "action=allow", "program="+exe, "enable=yes", "profile=any").Run(); err != nil {
		fmt.Println("      ! Lewati (perlu 'Run as administrator'). Dashboard tetap bisa dibuka di komputer ini.")
	} else {
		fmt.Println("      ✓ Firewall diizinkan")
	}

	fmt.Println("[5/5] Menjalankan aplikasi bel ...")
	start := exec.Command("wscript.exe", vbs)
	start.Dir = dir
	if err := start.Start(); err != nil {
		return fmt.Errorf("gagal menjalankan aplikasi: %w", err)
	}
	if waitUp("http://localhost:8080/api/version", 40*time.Second) {
		fmt.Println("      ✓ Aplikasi berjalan di http://localhost:8080")
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", "http://localhost:8080").Start()
	} else {
		fmt.Println("      ! Aplikasi belum menjawab. Coba buka shortcut 'Bel Sekolah' di Desktop sebentar lagi.")
	}

	fmt.Println()
	fmt.Println("==================================================================")
	fmt.Println(" SELESAI! Sistem bel sudah terpasang dan berjalan.")
	fmt.Println(" • Dashboard   : http://localhost:8080  (shortcut 'Bel Sekolah')")
	fmt.Println(" • Layar Kiosk : http://localhost:8080/kiosk")
	fmt.Println(" • PIN admin sama seperti di komputer asal installer ini dibuat.")
	fmt.Println(" • Update berikutnya: Pengaturan -> Update Aplikasi.")
	fmt.Println("==================================================================")
	return nil
}

func extract(dir string, keepData bool) (int, error) {
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return 0, fmt.Errorf("paket installer rusak: %w", err)
	}
	base, _ := filepath.Abs(dir)
	count := 0
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		target := filepath.Join(base, filepath.FromSlash(name))
		if !strings.HasPrefix(target, base+string(os.PathSeparator)) {
			continue
		}
		if keepData && strings.HasPrefix(name, "data/") {
			if name == "data/bell.db" {
				continue // keep existing schedules & settings
			}
			if _, err := os.Stat(target); err == nil {
				continue // keep existing audio file
			}
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return count, err
		}
		if name == "data/bell.db" {
			// stale WAL files of an old database must not be applied to the new one
			_ = os.Remove(target + "-wal")
			_ = os.Remove(target + "-shm")
		}
		if err := writeFile(f, target); err != nil {
			return count, fmt.Errorf("gagal menulis %s: %w", name, err)
		}
		count++
	}
	return count, nil
}

func writeFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func makeShortcuts(dir string) error {
	ps := fmt.Sprintf(`
$d = [Environment]::GetFolderPath('Desktop')
$w = New-Object -ComObject WScript.Shell
$s = $w.CreateShortcut((Join-Path $d 'Bel Sekolah.lnk'))
$s.TargetPath = 'http://localhost:8080'
$s.IconLocation = '%[1]s\bell-budut.exe,0'
$s.Save()
$k = $w.CreateShortcut((Join-Path $d 'Bel Sekolah - Layar Kiosk.lnk'))
$k.TargetPath = 'http://localhost:8080/kiosk'
$k.IconLocation = '%[1]s\bell-budut.exe,0'
$k.Save()
$r = $w.CreateShortcut((Join-Path $d 'Nyalakan Bel Sekolah.lnk'))
$r.TargetPath = 'wscript.exe'
$r.Arguments = '"%[1]s\Jalankan_Background.vbs"'
$r.WorkingDirectory = '%[1]s'
$r.IconLocation = '%[1]s\bell-budut.exe,0'
$r.Save()
`, dir)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq bell-budut.exe", "/NH").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "bell-budut.exe")
}

func waitUp(url string, timeout time.Duration) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if resp, err := client.Get(url); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

func ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func yes(prompt string, def bool) bool {
	a := strings.ToLower(ask(prompt))
	if a == "" {
		return def
	}
	return a == "y" || a == "ya" || a == "yes"
}
