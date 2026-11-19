package updater

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"4.6.0", "4.5.0", true},
		{"v4.10.0", "4.9.9", true},
		{"4.5.0", "4.5.0", false},
		{"4.4.9", "4.5.0", false},
		{"5.0", "4.99.99", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.a, c.b); got != c.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestExtractOnlyExeAndWeb(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "u.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for _, n := range []string{"bell-budut.exe", "web/templates/index.html", "data/bell.db", "Jalankan_Bel.bat", "../evil.txt"} {
		w, _ := zw.Create(n)
		w.Write([]byte("x"))
	}
	zw.Close()
	f.Close()

	stage := filepath.Join(dir, "stage")
	if err := extract(zp, stage); err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"bell-budut.exe", "web/templates/index.html"} {
		if _, err := os.Stat(filepath.Join(stage, must)); err != nil {
			t.Errorf("missing %s", must)
		}
	}
	for _, mustNot := range []string{"data/bell.db", "Jalankan_Bel.bat"} {
		if _, err := os.Stat(filepath.Join(stage, mustNot)); err == nil {
			t.Errorf("%s must not be extracted", mustNot)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Error("zip-slip file escaped")
	}
}
