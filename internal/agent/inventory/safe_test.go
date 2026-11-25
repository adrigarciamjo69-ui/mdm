package inventory

import (
	"strings"
	"testing"
)

func TestSafeCall_TurnsPanicIntoError(t *testing.T) {
	err := safeCall("SELECT ChassisTypes FROM Win32_SystemEnclosure", func() error {
		var x any = int32(3)
		_ = x.(uint16) // pánico como el de la biblioteca de WMI
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "Win32_SystemEnclosure") {
		t.Fatalf("el pánico debe volver como error: %v", err)
	}
	if err := safeCall("q", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestChassisCodes(t *testing.T) {
	if got := chassisCodes([]int32{10}); len(got) != 1 || got[0] != 10 {
		t.Fatalf("%v", got)
	}
}
