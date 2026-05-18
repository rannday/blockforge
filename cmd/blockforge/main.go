package main

import (
	"fmt"
	"os"

	"github.com/rannday/blockforge/internal/serverinstaller"
)

func main() {
	if err := serverinstaller.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
