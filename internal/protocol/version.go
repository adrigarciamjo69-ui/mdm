package protocol

import (
	"strings"
	"unicode"
)

// CompareVersions compara dos versiones y devuelve -1, 0 o 1. Las separa en
// segmentos por «.», «-» y «_», y cada segmento en tramos de cifras y de
// letras: las cifras se comparan como números («10» > «9», «007» = «7») y
// las letras como texto. Un segmento ausente vale 0 («24.04» = «24.04.0»).
func CompareVersions(a, b string) int {
	sa, sb := splitVersion(a), splitVersion(b)
	n := max(len(sa), len(sb))
	for i := 0; i < n; i++ {
		x, y := "0", "0"
		if i < len(sa) {
			x = sa[i]
		}
		if i < len(sb) {
			y = sb[i]
		}
		if c := compareSegment(x, y); c != 0 {
			return c
		}
	}
	return 0
}

func splitVersion(v string) []string {
	if v == "" {
		return nil
	}
	return strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
}

// compareSegment compara por tramos alternos de cifras y no cifras.
func compareSegment(a, b string) int {
	ta, tb := runs(a), runs(b)
	for i := 0; i < len(ta) && i < len(tb); i++ {
		x, y := ta[i], tb[i]
		dx, dy := isDigits(x), isDigits(y)
		var c int
		switch {
		case dx && dy:
			c = compareNumeric(x, y)
		case dx: // las cifras van antes que las letras
			c = -1
		case dy:
			c = 1
		default:
			c = strings.Compare(x, y)
		}
		if c != 0 {
			return c
		}
	}
	switch {
	case len(ta) < len(tb):
		return -1
	case len(ta) > len(tb):
		return 1
	}
	return 0
}

func runs(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || unicode.IsDigit(rune(s[i])) != unicode.IsDigit(rune(s[i-1])) {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
}

func isDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

func compareNumeric(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
