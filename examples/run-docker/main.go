// Command run-docker demonstrates executing code inside hardened Docker containers.
//
// Usage:
//
//	go run ./examples/run-docker [language] [optional_code]
//
// Examples:
//
//	go run ./examples/run-docker java
//	go run ./examples/run-docker go
//	go run ./examples/run-docker rust
//	go run ./examples/run-docker node
//	go run ./examples/run-docker bash
//	go run ./examples/run-docker bash 'echo "Custom script"; date'
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/docker"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/bash"
	"github.com/micromax/sandbox/packs/go"
	"github.com/micromax/sandbox/packs/java"
	"github.com/micromax/sandbox/packs/js"
	"github.com/micromax/sandbox/packs/node"
	"github.com/micromax/sandbox/packs/python"
	"github.com/micromax/sandbox/packs/rust"
)

func main() {
	// Target language from CLI (defaults to bash if unspecified)
	lang := "bash"
	if len(os.Args) > 1 {
		lang = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}

	// Code snippet from CLI (or default template)
	var code string
	if len(os.Args) > 2 {
		code = os.Args[2]
	} else {
		code = defaultSnippetFor(lang)
	}

	// 1. Initialize Wasm backend (default, pure Go)
	wBackend, err := wasm.New()
	if err != nil {
		log.Fatalf("failed to initialize wasm backend: %v", err)
	}

	// 2. Initialize Docker backend (opt-in for heavy/compiled runtimes)
	dBackend, err := docker.New()
	if err != nil {
		log.Fatalf("failed to initialize docker backend: %v", err)
	}
	defer dBackend.Close()

	// 3. Register packs
	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend, dBackend),
		sandbox.WithPacks(
			js.Pack(),
			python.Pack(),
			bash.Pack(),
			node.Pack(),
			golang.Pack(),
			rust.Pack(),
			java.Pack(),
		),
		// Prefer Wasm where supported (JS, Python); route heavy packs to Docker
		sandbox.WithPolicy(sandbox.PreferWasm),
	)
	if err != nil {
		log.Fatalf("failed to create sandbox: %v", err)
	}

	ctx := context.Background()

	fmt.Printf("=== Executing [%s] in Sandbox ===\n", lang)
	fmt.Printf("Code:\n%s\n\n", strings.TrimSpace(code))

	start := time.Now()
	res, err := sb.Run(ctx, sandbox.Spec{
		Lang: lang,
		Code: code,
		Limits: &sandbox.Limits{
			Memory:   512 << 20,        // 512MB to comfortably allow toolchain compilation (go, rust, java)
			WallTime: 60 * time.Second, // Allow enough time for initial image pulls if needed
		},
	})
	if err != nil {
		if errors.Is(err, sandbox.ErrBackendUnavailable) {
			fmt.Printf("Docker is not currently running on host (%v).\nStart Docker Desktop to execute containerized packs.\n", err)
			return
		}
		log.Fatalf("Execution error: %v", err)
	}

	fmt.Printf("--- Result ---\n")
	fmt.Printf("Backend:   %s\n", res.Backend)
	fmt.Printf("Exit Code: %d\n", res.ExitCode)
	fmt.Printf("Duration:  %v\n", time.Since(start))

	if len(res.Stdout) > 0 {
		fmt.Printf("\nStdout:\n%s", res.Stdout)
	}
	if len(res.Stderr) > 0 {
		fmt.Printf("\nStderr:\n%s", res.Stderr)
	}

	if len(res.Files) > 0 {
		fmt.Printf("\nGenerated files in /out: %d\n", len(res.Files))
		for name, content := range res.Files {
			fmt.Printf("  • %s (%d bytes): %s", name, len(content), string(content))
		}
	}
}

func defaultSnippetFor(lang string) string {
	switch lang {
	case "java":
		return `
public class Main {
    public static void main(String[] args) {
        System.out.println("Hello from Java 21 inside hardened Docker sandbox!");
        System.out.println("Java Vendor:  " + System.getProperty("java.vendor"));
        System.out.println("Java Version: " + System.getProperty("java.version"));
        System.out.println("Architecture: " + System.getProperty("os.arch"));
    }
}
`
	case "go", "golang":
		return `
package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println("Hello from Go inside hardened Docker sandbox!")
	fmt.Printf("Go Version:   %s\n", runtime.Version())
	fmt.Printf("OS / Arch:    %s / %s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("CPUs visible: %d\n", runtime.NumCPU())
}
`
	case "rust", "rs":
		return `
fn main() {
    println!("Hello from Rust inside hardened Docker sandbox!");
    let mut sum = 0;
    for i in 1..=10 { sum += i; }
    println!("Sum from 1 to 10 is: {}", sum);
}
`
	case "node", "nodejs":
		return `
console.log("Hello from Node.js inside hardened Docker sandbox!");
console.log("Node Version: " + process.version);
console.log("Platform:     " + process.platform + " (" + process.arch + ")");
`
	case "bash", "sh":
		return `
echo "Hello from Alpine Linux Bash in hardened Docker sandbox!"
uname -a
mkdir -p /out
echo "Generated at $(date)" > /out/timestamp.txt
`
	default:
		return `echo "Running default script"`
	}
}
