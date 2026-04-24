// AX-10 CLI driver for go-cache. Exercises cache.New + Set + Get round-trip
// against an in-memory Medium and exits non-zero on any mismatch.
//
//	task -d tests/cli/cache
//	go run ./tests/cli/cache
package main

import (
	"os"

	"dappco.re/go/cache"
	coreio "dappco.re/go/core/io"
)

func main() {
	medium := coreio.NewMockMedium()

	c, err := cache.New(medium, "/cache", cache.DefaultTTL)
	if err != nil {
		os.Exit(1)
	}

	payload := map[string]string{"hello": "world"}
	if err := c.Set("driver/roundtrip", payload); err != nil {
		os.Exit(2)
	}

	var out map[string]string
	found, err := c.Get("driver/roundtrip", &out)
	if err != nil {
		os.Exit(3)
	}
	if !found {
		os.Exit(4)
	}
	if out["hello"] != "world" {
		os.Exit(5)
	}
}
