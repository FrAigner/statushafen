package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
)

func TestPageRolesDiffer(t *testing.T) {
	before := []models.UserStatusPageRole{{StatusPageID: 1, RoleID: 2}, {StatusPageID: 3, RoleID: 4}}
	cases := []struct {
		name  string
		after []PageRoleInput
		want  bool
	}{
		{"gleich, andere Reihenfolge", []PageRoleInput{{StatusPageID: 3, RoleID: 4}, {StatusPageID: 1, RoleID: 2}}, false},
		{"gleich plus Eintrag ohne Rolle", []PageRoleInput{{StatusPageID: 1, RoleID: 2}, {StatusPageID: 3, RoleID: 4}, {StatusPageID: 9, RoleID: 0}}, false},
		{"Rolle getauscht", []PageRoleInput{{StatusPageID: 1, RoleID: 2}, {StatusPageID: 3, RoleID: 5}}, true},
		{"Seite entfernt", []PageRoleInput{{StatusPageID: 1, RoleID: 2}}, true},
		{"Seite hinzugefügt", []PageRoleInput{{StatusPageID: 1, RoleID: 2}, {StatusPageID: 3, RoleID: 4}, {StatusPageID: 5, RoleID: 6}}, true},
		{"alle entfernt", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pageRolesDiffer(before, tc.after))
		})
	}
	assert.False(t, pageRolesDiffer(nil, nil))
}
