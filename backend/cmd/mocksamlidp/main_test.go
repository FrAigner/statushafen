package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain_ReichtExitCodeVonRunWeiter(t *testing.T) {
	origExit, origRun := exit, run
	t.Cleanup(func() { exit, run = origExit, origRun })

	got := -1
	exit = func(code int) { got = code }
	run = func(getenv func(string) string) int {
		require.NotNil(t, getenv)
		return 3
	}

	main()

	require.Equal(t, 3, got)
}
