package patches

import (
	"testing"
)

const aptSim = `NOTE: This is only a simulation!
Reading package lists...
Calculating upgrade...
The following packages will be upgraded:
  libssl3t64 openssl vim vim-common
4 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
Inst libssl3t64 [3.0.13-0ubuntu3.1] (3.0.13-0ubuntu3.4 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])
Inst openssl [3.0.13-0ubuntu3.1] (3.0.13-0ubuntu3.4 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])
Inst vim-common [2:9.1.0016-1ubuntu7.2] (2:9.1.0016-1ubuntu7.3 Ubuntu:24.04/noble-updates [all])
Inst vim [2:9.1.0016-1ubuntu7.2] (2:9.1.0016-1ubuntu7.3 Ubuntu:24.04/noble-updates [amd64])
Conf libssl3t64 (3.0.13-0ubuntu3.4 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])
`

func TestParseAptSimulate(t *testing.T) {
	ups := parseAptSimulate(aptSim)
	if len(ups) != 4 {
		t.Fatalf("%+v", ups)
	}
	byID := map[string]bool{}
	for _, u := range ups {
		byID[u.ID] = u.Security
		if u.ID == "openssl" && (u.Title != "openssl 3.0.13-0ubuntu3.4" || u.Classification != "security") {
			t.Errorf("openssl: %+v", u)
		}
		if u.ID == "vim" && u.Classification != "update" {
			t.Errorf("vim: %+v", u)
		}
	}
	if !byID["openssl"] || !byID["libssl3t64"] || byID["vim"] || byID["vim-common"] {
		t.Fatalf("seguridad: %v", byID)
	}
}

const dnfCheck = `
kernel.x86_64                     5.14.0-427.42.1.el9_4       baseos
openssl.x86_64                    1:3.0.7-28.el9_4             baseos
python3-urllib3.noarch            1.26.5-5.el9_4.1             appstream
Obsoleting Packages
grub2-tools.x86_64                1:2.06-82.el9                baseos
    grub2-tools.x86_64            1:2.06-80.el9                @baseos
`

const dnfSecurity = `RLSA-2024:8922 Important/Sec. kernel-5.14.0-427.42.1.el9_4.x86_64
RLSA-2024:7848 Moderate/Sec.  openssl-1:3.0.7-28.el9_4.x86_64
`

func TestParseDnf(t *testing.T) {
	sec := parseDnfSecurity(dnfSecurity)
	if !sec["kernel"] || !sec["openssl"] || len(sec) != 2 {
		t.Fatalf("seguridad: %v", sec)
	}
	ups := parseDnfCheckUpdate(dnfCheck, sec)
	if len(ups) != 3 {
		t.Fatalf("las líneas de «Obsoleting» no son actualizaciones: %+v", ups)
	}
	for _, u := range ups {
		if (u.ID == "kernel" || u.ID == "openssl") != u.Security {
			t.Errorf("%+v", u)
		}
	}
}

func TestParseWindowsScan(t *testing.T) {
	out := "AVISO: algo\n" + `{"reboot_pending":true,"updates":[{"id":"6f1a3b20-1111-2222-3333-444455556666","kb":"KB5046617","title":"Actualización acumulativa","classification":"security","severity":"Critical","size_bytes":123456,"published_at":"2026-11-12T00:00:00Z","security":true}]}`
	scan, err := parseWindowsScan(out)
	if err != nil || !scan.RebootPending || len(scan.Updates) != 1 || scan.Updates[0].KB != "KB5046617" || !scan.Updates[0].Security {
		t.Fatalf("%+v %v", scan, err)
	}
	if _, err := parseWindowsScan(`{"error":"0x8024402C"}`); err == nil {
		t.Fatal("un error del script debe propagarse")
	}
}

// Salida real de scan.ps1 ejecutado con PowerShell 7 y un objeto COM simulado.
const realScanOutput = `{"updates":[{"id":"abc-123","kb":"KB5046617","title":"Actualización acumulativa","classification":"security","severity":"Critical","size_bytes":1234,"published_at":"2026-11-12T00:00:00.0000000Z","security":true}],"reboot_pending":false}`

func TestParseWindowsScan_RealScriptOutput(t *testing.T) {
	scan, err := parseWindowsScan(realScanOutput)
	if err != nil || len(scan.Updates) != 1 || scan.Updates[0].PublishedAt == nil || scan.Updates[0].Title != "Actualización acumulativa" {
		t.Fatalf("%+v %v", scan, err)
	}
}
