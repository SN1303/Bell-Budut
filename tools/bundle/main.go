// Command bundle builds installer/payload.zip: the app exe, web/, scripts, and a
// consistent snapshot of the CURRENT data (database with schedules & settings,
// plus all audio files), so the installer is ready to use right away.
//
// The payload contains school data — never commit it (see .gitignore).
package main

import (
	"archive/zip"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	exe := flag.String("exe", filepath.Join("dist", "bell-budut.exe"), "exe aplikasi yang akan dipaketkan")
	dbPath := flag.String("db", filepath.Join("data", "bell.db"), "database sumber")
	audioDir := flag.String("audio", filepath.Join("data", "audio"), "folder audio sumber")
	out := flag.String("out", filepath.Join("installer", "payload.zip"), "file zip keluaran")
	flag.Parse()

	// 1. Consistent DB snapshot (safe even while the app is running)
	snap := filepath.Join(os.TempDir(), "bell_snapshot.db")
	_ = os.Remove(snap)
	db, err := sql.Open("sqlite", *dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		log.Fatalf("buka database: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf("VACUUM INTO '%s'", strings.ReplaceAll(snap, "'", "''"))); err != nil {
		log.Fatalf("snapshot database: %v", err)
	}
	db.Close()
	defer os.Remove(snap)

	// 2. Write zip
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	zw := zip.NewWriter(f)

	add := func(src, name string) {
		if err := addFile(zw, src, name); err != nil {
			log.Fatalf("tambah %s: %v", src, err)
		}
	}

	add(*exe, "bell-budut.exe")
	add(snap, "data/bell.db")
	for _, s := range []string{"Jalankan_Bel.bat", "Jalankan_Background.vbs", "Pasang_AutoStart_Windows.bat", "Hapus_AutoStart_Windows.bat", "README.md"} {
		if _, err := os.Stat(s); err == nil {
			add(s, s)
		}
	}
	walk := func(root, prefix string) {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			add(p, prefix+filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			log.Fatal(err)
		}
	}
	walk("web", "web/")
	walk(*audioDir, "data/audio/")

	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	f.Close()
	st, _ := os.Stat(*out)
	fmt.Printf("Payload dibuat: %s (%.1f MB)\n", *out, float64(st.Size())/1024/1024)
}

func addFile(zw *zip.Writer, src, name string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	hdr, _ := zip.FileInfoHeader(info)
	hdr.Name = name
	hdr.Method = zip.Deflate
	if strings.HasSuffix(strings.ToLower(name), ".mp3") {
		hdr.Method = zip.Store // already compressed
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}
