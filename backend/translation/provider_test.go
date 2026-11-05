package translation

import "testing"

func TestNoopProvider_ReturnsInputUnchanged(t *testing.T) {
	var p Provider = NoopProvider{}
	got, err := p.Translate("Hallo Welt", "de", "en")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "Hallo Welt" {
		t.Fatalf("expected unchanged text, got %q", got)
	}
}
