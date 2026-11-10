// Command receiver is the entrypoint of the Mayfly receiver component.
package main

import (
	"log"

	"github.com/TeddyBertrand/Mayfly/internal/receiver"
)

// main runs the receiver component and exits non-zero on failure.
func main() {
	if err := receiver.Run(); err != nil {
		log.Fatal(err)
	}
}
