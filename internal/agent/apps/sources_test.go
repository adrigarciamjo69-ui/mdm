package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWingetLocate(t *testing.T) {
	pf := t.TempDir()
	for _, v := range []string{"1.21.3482.0", "1.9.25200.0", "1.23.1911.0"} {
		d := filepath.Join(pf, "WindowsApps", "Microsoft.DesktopAppInstaller_"+v+"_x64__8wekyb3d8bbwe")
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "winget.exe"), []byte("x"), 0o755)
	}
	_ = os.MkdirAll(filepath.Join(pf, "WindowsApps", "Microsoft.DesktopAppInstaller_1.99.0.0_arm64__8wekyb3d8bbwe"), 0o755)
	got, err := LocateWinget(pf)
	if err != nil || filepath.Base(filepath.Dir(got)) != "Microsoft.DesktopAppInstaller_1.23.1911.0_x64__8wekyb3d8bbwe" {
		t.Fatalf("debe elegir la versión más alta de x64: %s %v", got, err)
	}
	if _, err := LocateWinget(t.TempDir()); err == nil {
		t.Fatal("sin winget debe dar error")
	}
}

func TestPackageCommands(t *testing.T) {
	cases := []struct {
		source, intent, pkg string
		keep                bool
		want                string
	}{
		{"apt", "required", "nginx", false, "apt-get install -y nginx"},
		{"apt", "required", "nginx", true, "apt-get install -y --only-upgrade nginx"},
		{"apt", "uninstall", "nginx", false, "apt-get remove -y nginx"},
		{"dnf", "required", "htop", false, "dnf install -y htop"},
		{"dnf", "required", "htop", true, "dnf upgrade -y htop"},
		{"dnf", "uninstall", "htop", false, "dnf remove -y htop"},
		{"winget", "required", "Mozilla.Firefox", false, `"WINGET" install --id Mozilla.Firefox --exact --silent --scope machine --accept-package-agreements --accept-source-agreements --disable-interactivity`},
		{"winget", "required", "Mozilla.Firefox", true, `"WINGET" upgrade --id Mozilla.Firefox --exact --silent --accept-package-agreements --accept-source-agreements --disable-interactivity`},
		{"winget", "uninstall", "Mozilla.Firefox", false, `"WINGET" uninstall --id Mozilla.Firefox --exact --silent --disable-interactivity`},
	}
	for _, c := range cases {
		if got := packageCommand(c.source, c.intent, c.pkg, c.keep, "WINGET"); got != c.want {
			t.Errorf("%s %s keep=%v:\n %s\n %s", c.source, c.intent, c.keep, got, c.want)
		}
	}
}
