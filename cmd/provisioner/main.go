// Command provisioner is the entrypoint of the Mayfly provisioner component.
package main

import (
	"log"

	"github.com/TeddyBertrand/Mayfly/internal/provisioner"
)

// main runs the provisioner component and exits non-zero on failure.
func main() {
	if err := provisioner.Run(); err != nil {
		log.Fatal(err)
	}
}
