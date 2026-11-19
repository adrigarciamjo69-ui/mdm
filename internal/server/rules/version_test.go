package rules

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"10.0.26100", "10.0.22631", 1}, {"10.0.9", "10.0.10", -1}, {"24.04", "24.04.0", 0},
		{"1.0.0-rc1", "1.0.0-rc2", -1}, {"", "1", -1}, {"5.2.21-2ubuntu4", "5.2.21-2ubuntu10", -1},
		{"1.2.3", "1.2.3", 0}, {"2", "10", -1}, {"1.10", "1.9", 1}, {"007", "7", 0},
		{"1.2.3_beta", "1.2.3_beta", 0}, {"R2EET41W", "R2EET40W", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, se esperaba %d", c.a, c.b, got, c.want)
		}
		if got := CompareVersions(c.b, c.a); got != -c.want {
			t.Errorf("no es antisimétrica para (%q, %q)", c.b, c.a)
		}
	}
}
