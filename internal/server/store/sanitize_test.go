package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanString(t *testing.T) {
	if got := CleanString("Evil\x00App"); got != "EvilApp" {
		t.Errorf("nulos: %q", got)
	}
	if got := CleanString(strings.Repeat("a", 5000)); len(got) != 512 {
		t.Errorf("recorte: %d", len(got))
	}
	if got := CleanString("normal"); got != "normal" {
		t.Errorf("cadena normal alterada: %q", got)
	}
	if got := CleanString("mal\xff\xfeformado"); !utf8.ValidString(got) {
		t.Errorf("UTF-8 inválido no saneado: %q", got)
	}
}

func TestSanitize_MultibyteTruncation(t *testing.T) {
	got := CleanString(strings.Repeat("ñ", 600))
	if utf8.RuneCountInString(got) != 512 || !utf8.ValidString(got) || got != strings.Repeat("ñ", 512) {
		t.Fatalf("recorte multibyte: %d runas, válido=%v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
}
