package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain_ReichtExitCodeVonRunWeiter(t *testing.T) {
	origExit, origArgs := exit, os.Args
	t.Cleanup(func() { exit, os.Args = origExit, origArgs })

	got := -1
	exit = func(code int) { got = code }
	os.Args = []string{"workflowlint"}

	main()

	require.Equal(t, 2, got)
}
