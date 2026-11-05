package handlers

import "testing"

// Grenzfälle von emailMatchesAutoJoin. Eine E-Mail ohne lokalen
// Teil ("@acme.example") ist keine gültige Adresse und darf nie Auto-Join
// auslösen; ein einzelnes Zeichen davor genügt.
func TestEmailMatchesAutoJoinGrenzfaelle(t *testing.T) {
	cases := []struct {
		name    string
		email   string
		domains string
		want    bool
	}{
		{"Treffer", "a@acme.example", "acme.example", true},
		{"Treffer, Gross-/Kleinschreibung", "a@ACME.example", "acme.example", true},
		{"zweiter Eintrag der Liste", "a@b.example", "acme.example,b.example", true},
		{"leerer lokaler Teil", "@acme.example", "acme.example", false},
		{"kein At-Zeichen", "acme.example", "acme.example", false},
		{"leere Liste", "a@acme.example", "", false},
		{"Subdomain zählt nicht", "a@sub.acme.example", "acme.example", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := emailMatchesAutoJoin(c.email, c.domains); got != c.want {
				t.Fatalf("emailMatchesAutoJoin(%q, %q) = %v, erwartet %v", c.email, c.domains, got, c.want)
			}
		})
	}
}
