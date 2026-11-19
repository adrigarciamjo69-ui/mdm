package inventory

import "testing"

func TestChooseHardwareID(t *testing.T) {
	cases := []struct{ smbios, fallback, want string }{
		{"4C4C4544-0042-3510-8054-B4C04F564433", "abc", "4c4c4544-0042-3510-8054-b4c04f564433"},
		{"  4C4C4544-0042-3510-8054-B4C04F564433\n", "abc", "4c4c4544-0042-3510-8054-b4c04f564433"},
		{"", "abc", "abc"},
		{"00000000-0000-0000-0000-000000000000", "abc", "abc"},
		{"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF", "abc", "abc"},
		{"03000200-0400-0500-0006-000700080009", "abc", "abc"},
		{"Not Settable", "abc", "abc"},
		{"To Be Filled By O.E.M.", "abc", "abc"},
		{"no-es-un-uuid", " ABC \n", "abc"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := chooseHardwareID(c.smbios, c.fallback); got != c.want {
			t.Errorf("chooseHardwareID(%q, %q) = %q, se esperaba %q", c.smbios, c.fallback, got, c.want)
		}
	}
}
