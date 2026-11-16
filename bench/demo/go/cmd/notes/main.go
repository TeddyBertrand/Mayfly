// Command notes serves the demo notes API, so the Go workflow builds a real binary.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	notes "github.com/TeddyBertrand/mayfly-demo/go"
)

const (
	defaultAddr       = "127.0.0.1:8080"
	readHeaderTimeout = 5 * time.Second
)

// main starts the API on NOTES_ADDR, or on loopback port 8080 by default.
func main() {
	addr := os.Getenv("NOTES_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           notes.Router(notes.NewStore(time.Now)),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	log.Printf("notes listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
