package main

import (
	"fmt"
	"io"

	"github.com/TeddyBertrand/Mayfly/internal/config"
)

// version is the release version, set at build time with -ldflags "-X main.version=<v>".
var version = config.DevVersion

// printVersion writes the version to w and reports true when args ask for it.
func printVersion(args []string, w io.Writer) bool {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-version") {
		_, _ = fmt.Fprintln(w, version)
		return true
	}
	return false
}
