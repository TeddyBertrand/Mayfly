// Command reaper is the entrypoint of the Mayfly reaper component.
package main

import (
	"log"

	"github.com/TeddyBertrand/Mayfly/internal/reaper"
)

// main runs the reaper component and exits non-zero on failure.
func main() {
	if err := reaper.Run(); err != nil {
		log.Fatal(err)
	}
}
