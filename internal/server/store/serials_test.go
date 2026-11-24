package store_test

import (
	"testing"

	"mdm/internal/server/store"
)

func TestSerialKey(t *testing.T) {
	cases := map[string]string{
		" abc123 ":                             "ABC123",
		"pf-3x9k2l":                            "PF-3X9K2L",
		"":                                     "",
		"   ":                                  "",
		"0":                                    "",
		"123":                                  "",
		"0000000":                              "",
		"XXXXXXXX":                             "",
		"To be filled by O.E.M.":               "",
		"Default string":                       "",
		"System Serial Number":                 "",
		"Chassis Serial Number":                "",
		"Not Specified":                        "",
		"None":                                 "",
		"N/A":                                  "",
		"0123456789":                           "",
		"VMware-56 4d 1a 2b 3c 4d 5e 6f-01 02": "",
		"Parallels-12 34 56":                   "",
	}
	for in, want := range cases {
		if got := store.SerialKey(in); got != want {
			t.Errorf("SerialKey(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}
