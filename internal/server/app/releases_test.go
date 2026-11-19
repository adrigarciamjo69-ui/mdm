package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSums(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReleases_ParsesSums(t *testing.T) {
	dir := writeSums(t, "aaa111  mdm-agent-linux-amd64\nbbb222 *mdm-agent-windows-amd64.exe\nbasura\nccc333  otro-fichero\n")
	r, err := LoadReleases(dir, "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := r.For("linux", "amd64")
	if !ok || u.Version != "1.2.0" || u.SHA256 != "aaa111" || u.URL != "/downloads/mdm-agent-linux-amd64" {
		t.Fatalf("linux: %+v %v", u, ok)
	}
	u, ok = r.For("windows", "amd64")
	if !ok || u.SHA256 != "bbb222" || u.URL != "/downloads/mdm-agent-windows-amd64.exe" {
		t.Fatalf("windows: %+v %v", u, ok)
	}
	if _, ok := r.For("linux", "arm64"); ok {
		t.Fatal("sin binario de arm64 no hay actualización")
	}
	if r.Version() != "1.2.0" || len(r.Binaries()) != 2 {
		t.Fatalf("versión o binarios: %s %v", r.Version(), r.Binaries())
	}
}

func TestReleases_MissingSumsFile(t *testing.T) {
	r, err := LoadReleases(writeSums(t, ""), "1.2.0")
	if err != nil {
		t.Fatalf("sin fichero de sumas no debe fallar: %v", err)
	}
	if _, ok := r.For("linux", "amd64"); ok {
		t.Fatal("sin sumas no se ofrecen actualizaciones")
	}
}

func TestReleases_DevVersionNeverOffers(t *testing.T) {
	r, _ := LoadReleases(writeSums(t, "aaa  mdm-agent-linux-amd64\n"), "dev")
	if _, ok := r.For("linux", "amd64"); ok {
		t.Fatal("un servidor «dev» no debe ofrecer actualizaciones")
	}
}

func TestReleases_NilSafe(t *testing.T) {
	var r *Releases
	if r.Version() != "" || len(r.Binaries()) != 0 {
		t.Fatal("un *Releases nulo debe comportarse como vacío")
	}
	if _, ok := r.For("linux", "amd64"); ok {
		t.Fatal("nulo no ofrece nada")
	}
}
