package patches

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"mdm/internal/protocol"
)

// aptInst reconoce «Inst <paquete> [<actual>] (<nueva> <orígenes> [arq])».
var aptInst = regexp.MustCompile(`^Inst (\S+) (?:\[[^\]]*\] )?\((\S+) ([^\[]*)\[`)

func parseAptSimulate(out string) []protocol.UpdateInfo {
	var ups []protocol.UpdateInfo
	for _, line := range strings.Split(out, "\n") {
		m := aptInst.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		sec := strings.Contains(m[3], "-security")
		class := "update"
		if sec {
			class = "security"
		}
		ups = append(ups, protocol.UpdateInfo{ID: m[1], Title: m[1] + " " + m[2], Classification: class, Security: sec})
	}
	return ups
}

// dnfNVRA quita versión, revisión y arquitectura a «nombre-ver-rev.arq».
var dnfNVRA = regexp.MustCompile(`^(.+)-[^-]+-[^-]+\.[^.]+$`)

func parseDnfSecurity(out string) map[string]bool {
	sec := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		if m := dnfNVRA.FindStringSubmatch(f[len(f)-1]); m != nil {
			sec[m[1]] = true
		}
	}
	return sec
}

func parseDnfCheckUpdate(out string, security map[string]bool) []protocol.UpdateInfo {
	var ups []protocol.UpdateInfo
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Obsoleting") {
			break // lo que sigue son paquetes obsoletos, no actualizaciones
		}
		f := strings.Fields(line)
		if len(f) != 3 || strings.HasPrefix(line, " ") {
			continue
		}
		name := f[0]
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i]
		}
		sec := security[name]
		class := "update"
		if sec {
			class = "security"
		}
		ups = append(ups, protocol.UpdateInfo{ID: name, Title: name + " " + f[1], Classification: class, Security: sec})
	}
	return ups
}

// lastJSONLine devuelve la última línea que empieza por «{».
func lastJSONLine(out string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "{") {
			return l, nil
		}
	}
	return "", fmt.Errorf("el script de Windows Update no devolvió resultado: %s", strings.TrimSpace(out))
}

func parseWindowsScan(out string) (protocol.UpdateScan, error) {
	var r struct {
		protocol.UpdateScan
		Error string `json:"error"`
	}
	line, err := lastJSONLine(out)
	if err != nil {
		return r.UpdateScan, err
	}
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return r.UpdateScan, err
	}
	if r.Error != "" {
		return r.UpdateScan, errors.New("Windows Update: " + r.Error)
	}
	return r.UpdateScan, nil
}

func parseWindowsInstall(out string) (protocol.UpdateResults, error) {
	var r struct {
		protocol.UpdateResults
		Error string `json:"error"`
	}
	line, err := lastJSONLine(out)
	if err != nil {
		return r.UpdateResults, err
	}
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return r.UpdateResults, err
	}
	if r.Error != "" {
		return r.UpdateResults, errors.New("Windows Update: " + r.Error)
	}
	return r.UpdateResults, nil
}
