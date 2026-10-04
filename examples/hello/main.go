// Command hello shows the shape of the sandbox API.
//
// It uses the in-repo fake backend, which provides NO isolation, so it can
// only demonstrate the API. Real Wasm isolation arrives in milestone M2.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/internal/fake"
)

func main() {
	sb, err := sandbox.New(
		sandbox.WithBackends(fake.Backend{}),
		sandbox.WithPacks(fake.Packs()...),
		sandbox.WithPolicy(sandbox.NewPolicy("fake")),
	)
	if err != nil {
		log.Fatal(err)
	}

	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Code: "hello from the sandbox\n"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("stdout=%q exit=%d backend=%s\n", res.Stdout, res.ExitCode, res.Backend)

	// Limits are enforced: this "program" never finishes.
	_, err = sb.Run(context.Background(), sandbox.Spec{
		Lang:   "spin",
		Limits: &sandbox.Limits{WallTime: 100 * time.Millisecond},
	})
	fmt.Println("timed out:", errors.Is(err, sandbox.ErrTimeout))
}
