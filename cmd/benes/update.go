package main

import (
	"fmt"
	"io"
)

const updatePolicy = `Benes does not self-update in-process.
Install or upgrade with your package manager, or rebuild from source.
The live installation is never mutated by benes update.
`

func runUpdate(args []string, stdout, stderr io.Writer) int {
	_ = args
	fmt.Fprint(stdout, updatePolicy)
	return 0
}
