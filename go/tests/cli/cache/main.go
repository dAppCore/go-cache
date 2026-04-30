// AX-10 CLI driver for go-cache. Exercises cache.New + Set + Get round-trip
// against an in-memory Medium and exits non-zero on any mismatch.
//
//	task -d tests/cli/cache
//	go run ./tests/cli/cache
package main

import (
	core "dappco.re/go"
	"dappco.re/go/cache"
	coreio "dappco.re/go/io"
)

func main() {
	medium := coreio.NewMockMedium()

	cacheResult := cache.New(medium, "/cache", cache.DefaultTTL)
	if !cacheResult.OK {
		core.Exit(1)
	}
	c := cacheResult.Value.(*cache.Cache)

	payload := map[string]string{"hello": "world"}
	if r := c.Set("driver/roundtrip", payload); !r.OK {
		core.Exit(2)
	}

	var out map[string]string
	found := c.Get("driver/roundtrip", &out)
	if !found.OK {
		core.Exit(3)
	}
	if !found.Value.(bool) {
		core.Exit(4)
	}
	if out["hello"] != "world" {
		core.Exit(5)
	}
}
