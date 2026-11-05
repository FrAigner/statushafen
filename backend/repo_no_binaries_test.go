package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Regression Ein lokales "go build" in backend/cmd/telegrambot/
// legte dort ein 14-MB-Binary ab, das eingecheckt wurde. Der Test
// prüft alle von git verfolgten Dateien im Backend auf ausführbare Formate
// (ELF, Mach-O, PE) - .gitignore allein hält ein "git add -f" nicht auf.
// Geprüft wird der Index, nicht das Dateisystem: Ein lokal gebautes, nicht
// eingechecktes Binary soll den Test nicht rot machen.
func TestKeineEingechecktenBinaries(t *testing.T) {
	out, err := exec.Command("git", "ls-files", "-z", ".").Output()
	if err != nil {
		t.Skipf("git ls-files nicht verfügbar: %v", err)
	}
	for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if f == "" {
			continue
		}
		if kind := binaryKind(f); kind != "" {
			t.Errorf("%s ist ein eingechecktes %s-Binary - entfernen (git rm --cached) und Build-Ausgaben nicht committen", f, kind)
		}
	}
}

// binaryKind liefert das ausführbare Format anhand der Magic Bytes, sonst "".
func binaryKind(path string) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = fh.Close() }()
	head := make([]byte, 4)
	if n, _ := fh.Read(head); n < 4 {
		return ""
	}
	return binaryKindOf(head)
}

func binaryKindOf(head []byte) string {
	switch {
	case bytes.Equal(head, []byte{0x7f, 'E', 'L', 'F'}):
		return "ELF"
	case bytes.Equal(head, []byte{0xcf, 0xfa, 0xed, 0xfe}), bytes.Equal(head, []byte{0xce, 0xfa, 0xed, 0xfe}),
		bytes.Equal(head, []byte{0xfe, 0xed, 0xfa, 0xcf}), bytes.Equal(head, []byte{0xca, 0xfe, 0xba, 0xbe}):
		return "Mach-O"
	case bytes.Equal(head[:2], []byte{'M', 'Z'}):
		return "PE"
	}
	return ""
}

func TestBinaryKindOf(t *testing.T) {
	tests := map[string]struct {
		head []byte
		want string
	}{
		"ELF":          {[]byte{0x7f, 'E', 'L', 'F'}, "ELF"},
		"Mach-O arm64": {[]byte{0xcf, 0xfa, 0xed, 0xfe}, "Mach-O"},
		"Mach-O 32":    {[]byte{0xce, 0xfa, 0xed, 0xfe}, "Mach-O"},
		"Mach-O BE":    {[]byte{0xfe, 0xed, 0xfa, 0xcf}, "Mach-O"},
		"Mach-O fat":   {[]byte{0xca, 0xfe, 0xba, 0xbe}, "Mach-O"},
		"PE":           {[]byte{'M', 'Z', 0x90, 0x00}, "PE"},
		"Go-Quelltext": {[]byte("pack"), ""},
		"JSON":         {[]byte("{\n  "), ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := binaryKindOf(tt.head); got != tt.want {
				t.Errorf("binaryKindOf(%q) = %q, want %q", tt.head, got, tt.want)
			}
		})
	}
}

func TestBinaryKind_ErkenntDateiUndUeberspringtKurzeOderFehlende(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if got := binaryKind(write("bin", []byte{0xcf, 0xfa, 0xed, 0xfe, 0x0c})); got != "Mach-O" {
		t.Errorf("Mach-O-Datei: %q", got)
	}
	if got := binaryKind(write("kurz", []byte{0x7f})); got != "" {
		t.Errorf("zu kurze Datei: %q", got)
	}
	if got := binaryKind(dir + "/fehlt"); got != "" {
		t.Errorf("fehlende Datei: %q", got)
	}
}
