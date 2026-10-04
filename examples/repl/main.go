// Command repl demonstrates interactive stateful REPL sessions in Python and JavaScript.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/js"
	"github.com/micromax/sandbox/packs/python"
)

func main() {
	lang := "js"
	if len(os.Args) > 1 {
		lang = os.Args[1]
	}

	wBackend, err := wasm.New()
	if err != nil {
		log.Fatalf("failed to initialize wasm backend: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack(), python.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		log.Fatalf("failed to initialize sandbox: %v", err)
	}

	ctx := context.Background()

	// 1. Programmatic session demonstration
	fmt.Printf("=== Programmatic Session Demo (%s) ===\n", lang)
	sess, err := sb.NewSession(ctx, lang, sandbox.WithSessionLimits(sandbox.Limits{
		WallTime: 5 * time.Second,
	}))
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}
	defer sess.Close()

	fmt.Printf("Session started: %s (ID: %s)\n", sess.Lang(), sess.ID())

	var snippets []string
	if lang == "python" {
		snippets = []string{
			"x = 21",
			"def double(n): return n * 2",
			"print('Result:', double(x))",
		}
	} else {
		snippets = []string{
			"var x = 21;",
			"function double(n) { return n * 2; }",
			"console.log('Result: ' + double(x));",
		}
	}

	for _, code := range snippets {
		fmt.Printf("\n>>> %s\n", code)
		res, err := sess.Eval(ctx, code)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		if len(res.Stdout) > 0 {
			fmt.Printf("%s", res.Stdout)
		}
		if len(res.Stderr) > 0 {
			fmt.Printf("[stderr] %s", res.Stderr)
		}
	}

	// 2. Interactive REPL loop if stdin is interactive or user typed 'repl'
	if len(os.Args) > 2 && os.Args[2] == "--interactive" {
		fmt.Printf("\n=== Interactive REPL (%s) === (Type 'exit' to quit)\n", lang)
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("sandbox> ")
			if !scanner.Scan() {
				break
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "exit" || line == "quit" {
				break
			}
			if line == "" {
				continue
			}

			evalCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			res, err := sess.Eval(evalCtx, line)
			cancel()

			if err != nil {
				fmt.Printf("Error: %v\n", err)
				if !sess.Alive() {
					fmt.Println("Session terminated. Exiting.")
					break
				}
				continue
			}

			if len(res.Stdout) > 0 {
				fmt.Print(string(res.Stdout))
			}
			if len(res.Stderr) > 0 {
				fmt.Printf("[stderr] %s", string(res.Stderr))
			}
		}
	}
}
