//go:build integration

package blockforge_test

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	root, err := integrationTempRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration temp root: %v\n", err)
		os.Exit(1)
	}
	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintf(os.Stderr, "clean integration temp root: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
