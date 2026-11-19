package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckinResponse_EmptyCommandsIsArray(t *testing.T) {
	b, err := json.Marshal(CheckinResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"commands":[]`) {
		t.Fatalf("commands no es []: %s", b)
	}
}

func TestEnrollRequest_JSONNames(t *testing.T) {
	b, _ := json.Marshal(EnrollRequest{Token: "t", HardwareID: "h", OSName: "o", AgentVersion: "1"})
	for _, k := range []string{`"token"`, `"hardware_id"`, `"os_name"`, `"os_version"`, `"agent_version"`, `"hostname"`, `"platform"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("falta la clave %s en %s", k, b)
		}
	}
}

func TestInventory_JSONNames(t *testing.T) {
	b, _ := json.Marshal(sampleInventory())
	for _, k := range []string{`"collected_at"`, `"unavailable"`, `"size_bytes"`, `"free_bytes"`, `"disk_encryption"`, `"antivirus_product"`, `"secure_boot"`, `"installed_at"`, `"logged_in_user"`, `"memory_bytes"`, `"ipv4"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("falta la clave %s", k)
		}
	}
}
