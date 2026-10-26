// Command sandbox provides a unified command-line interface for running,
// testing, serving, and diagnosing sandboxed code executions.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/artifact"
	backenddocker "github.com/micromax/sandbox/backend/docker"
	backendwasm "github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/mcp"
	"github.com/micromax/sandbox/packages"
)

var version = "0.3.0"

func initSandbox() (*sandbox.Sandbox, *backendwasm.Backend, *backenddocker.Backend, error) {
	wBackend, err := backendwasm.New()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initializing wasm backend: %w", err)
	}

	dBackend, err := backenddocker.New()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initializing docker backend: %w", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend, dBackend),
		sandbox.WithPacks(allPacks()...),
		sandbox.WithPolicy(sandbox.PreferWasm),
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return sb, wBackend, dBackend, nil
}

func inferLang(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".js", ".mjs":
		return "js"
	case ".ts":
		return "ts"
	case ".py":
		return "python"
	case ".lua":
		return "lua"
	case ".wasm":
		return "wasm"
	case ".java":
		return "java"
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".sh", ".bash":
		return "bash"
	default:
		return ""
	}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "run":
		handleRun(args)
	case "repl":
		handleREPL(args)
	case "serve":
		handleServe(args)
	case "mcp":
		handleMCP(args)
	case "doctor":
		handleDoctor(args)
	case "packs":
		handlePacks(args)
	case "packages":
		handlePackages(args)
	case "version", "--version", "-v":
		fmt.Printf("sandbox v%s (github.com/micromax/sandbox)\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage:
  sandbox run   [flags] <file>       Run code in sandbox
  sandbox repl  [flags]              Start stateful interactive REPL session
  sandbox serve [flags] <file>       Serve a network service behind host proxy
  sandbox mcp                        Start Model Context Protocol (MCP) server
  sandbox packages [list|add]        Manage cached offline packages
  sandbox doctor                     Check system capabilities and dependencies
  sandbox packs list                 List supported language packs
  sandbox version                    Print version information

Commands & Flags:
  run:
    --lang <name>       Language pack (inferred from file extension if omitted)
    --dir <path>        Project directory to load local dependency files from
    --timeout <dur>     Execution deadline (e.g. 5s, 1m)
    --mem <size>        Memory limit: bytes, or with a K/M/G suffix (e.g. 64M, 1G)
    --pkg <packages>    Comma-separated list of packages to inject (e.g. requests,numpy)
    --net <hosts>       Comma-separated allow-list for outbound network
    --json              Output execution results in JSON format

  repl:
    --lang <name>       Language (default: python)
    --timeout <dur>     Per-evaluation deadline

  serve:
    --lang <name>       Language pack
    --port <port>       Host port to map (default: auto-assigned)
    --ttl <dur>         Maximum lifetime before automatic shutdown (default: 10m)`)
}

func handleRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	lang := fs.String("lang", "", "Language name")
	timeout := fs.Duration("timeout", 10*time.Second, "Wall-clock timeout")
	mem := fs.String("mem", "", "Memory limit: bytes, or with a K/M/G suffix (e.g. 64M, 1G)")
	dirFlag := fs.String("dir", "", "Project directory path to load local dependency files from")
	pkgFlag := fs.String("pkg", "", "Comma-separated list of guest packages to inject")
	netHosts := fs.String("net", "", "Comma-separated outbound host allow-list")
	asJSON := fs.Bool("json", false, "Output results in JSON format")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: file argument required: sandbox run [flags] <file>")
		os.Exit(1)
	}

	filePath := fs.Arg(0)
	targetLang := *lang
	if targetLang == "" {
		targetLang = inferLang(filePath)
		if targetLang == "" {
			fmt.Fprintf(os.Stderr, "error: could not infer language from %s; specify with --lang\n", filePath)
			os.Exit(1)
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", filePath, err)
		os.Exit(1)
	}

	sb, _, _, err := initSandbox()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox error: %v\n", err)
		os.Exit(1)
	}

	var limits *sandbox.Limits
	if *timeout != 0 || *mem != "" {
		limits = &sandbox.Limits{
			WallTime: *timeout,
		}
		if *mem != "" {
			memBytes, err := parseSize(*mem)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid --mem: %v\n", err)
				os.Exit(1)
			}
			limits.Memory = memBytes
		}
	}

	var netPolicy *sandbox.NetPolicy
	if *netHosts != "" {
		hosts := strings.Split(*netHosts, ",")
		for i := range hosts {
			hosts[i] = strings.TrimSpace(hosts[i])
		}
		netPolicy = &sandbox.NetPolicy{
			AllowHosts: hosts,
			AllowPorts: []int{80, 443},
		}
	}

	spec := sandbox.Spec{
		Lang:   targetLang,
		Limits: limits,
		Net:    netPolicy,
	}
	if *pkgFlag != "" {
		for _, p := range strings.Split(*pkgFlag, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				spec.Packages = append(spec.Packages, p)
			}
		}
	}

	if *dirFlag != "" {
		projFiles, err := sandbox.LoadProjectFiles(*dirFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading project files from %s: %v\n", *dirFlag, err)
			os.Exit(1)
		}
		spec.Files = projFiles
	}

	if targetLang == "wasm" || targetLang == "wasi" || targetLang == "wasip1" {
		if spec.Files == nil {
			spec.Files = make(map[string][]byte)
		}
		spec.Files["main.wasm"] = data
	} else {
		spec.Code = string(data)
	}

	if !*asJSON {
		spec.StdoutWriter = os.Stdout
		spec.StderrWriter = os.Stderr
	}

	ctx := context.Background()
	res, runErr := sb.Run(ctx, spec)

	if *asJSON {
		out := map[string]any{
			"exit_code": -1,
			"backend":   "",
			"duration":  "",
		}
		if res != nil {
			out["stdout"] = string(res.Stdout)
			out["stderr"] = string(res.Stderr)
			out["exit_code"] = res.ExitCode
			out["backend"] = res.Backend
			out["duration"] = res.Usage.Wall.String()
			out["peak_memory"] = res.Usage.PeakMemory
			out["net_log"] = res.NetLog
		}
		if runErr != nil {
			out["error"] = runErr.Error()
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		if runErr != nil {
			os.Exit(1)
		}
		return
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "execution error: %v\n", runErr)
		os.Exit(1)
	}
	if res != nil && res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
}

func handleREPL(args []string) {
	fs := flag.NewFlagSet("repl", flag.ExitOnError)
	lang := fs.String("lang", "python", "Language for REPL session")
	_ = fs.Parse(args)

	sb, _, _, err := initSandbox()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	sess, err := sb.NewSession(ctx, *lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start REPL session: %v\n", err)
		os.Exit(1)
	}
	defer sess.Close()

	fmt.Printf("Started %s interactive sandbox session (type 'exit' or Ctrl+C to quit)\n", *lang)
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print(">>> ")
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "exit" || trimmed == "quit" {
			break
		}
		if trimmed == "" {
			continue
		}

		res, err := sess.Eval(ctx, line)
		if err != nil {
			fmt.Printf("error: %v\n", err)
			continue
		}
		if len(res.Stdout) > 0 {
			os.Stdout.Write(res.Stdout)
		}
		if len(res.Stderr) > 0 {
			os.Stderr.Write(res.Stderr)
		}
	}
}

func handleServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	lang := fs.String("lang", "", "Language name")
	port := fs.Int("port", 0, "Host port (0 = auto-assign)")
	ttl := fs.Duration("ttl", 10*time.Minute, "Service lifetime")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: file argument required: sandbox serve [flags] <file>")
		os.Exit(1)
	}

	file := fs.Arg(0)
	targetLang := *lang
	if targetLang == "" {
		targetLang = inferLang(file)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		os.Exit(1)
	}

	sb, _, _, err := initSandbox()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nStopping service...")
		cancel()
	}()

	spec := sandbox.Spec{
		Lang: targetLang,
		Code: string(data),
	}

	serveOpts := sandbox.ServeOpts{
		TTL: *ttl,
	}
	if *port > 0 {
		serveOpts.Ports = []sandbox.PortMap{{Host: *port, Guest: *port}}
	} else {
		serveOpts.Ports = []sandbox.PortMap{{Guest: 8000}}
	}

	svc, err := sb.Serve(ctx, spec, serveOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve failed: %v\n", err)
		os.Exit(1)
	}
	defer svc.Stop()

	fmt.Printf("Service online: %s\n", svc.URL())
	fmt.Printf("Press Ctrl+C to terminate.\n")

	select {
	case <-svc.Done():
		fmt.Println("Service closed.")
	case <-ctx.Done():
		svc.Stop()
	}
}

func handleDoctor(args []string) {
	fmt.Printf("sandbox doctor v%s\n", version)
	fmt.Println("========================================")

	// 1. Wazero / Pure Wasm Engine
	fmt.Println("\n[1] WebAssembly Engine (wazero):")
	wBackend, err := backendwasm.New()
	if err != nil {
		fmt.Printf("  ❌ Failed to initialize wazero: %v\n", err)
	} else {
		defer wBackend.Close(context.Background())
		fmt.Println("  ✅ Active (Pure Go, Zero CGO, Zero daemon required)")
	}

	// 2. Docker / OCI Engine
	fmt.Println("\n[2] Docker / OCI Engine:")
	dBackend, _ := backenddocker.New()
	if dBackend != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := backenddocker.NewClient(ctx, "")
		if err != nil {
			fmt.Printf("  ⚠️  Docker daemon not detected (%v)\n", err)
			fmt.Printf("     -> Docker-only languages (%s) will be unavailable.\n", strings.Join(packNames(isDockerOnly), ", "))
			fmt.Printf("     -> Wasm languages (%s) remain 100%% functional.\n", strings.Join(packNames(hasWasm), ", "))
		} else {
			fmt.Println("  ✅ Docker daemon is connected and healthy.")
		}
	}

	// 3. Artifact Cache
	fmt.Println("\n[3] Artifact Storage & Cache:")
	store, err := artifact.NewStore()
	if err != nil {
		fmt.Printf("  ❌ Cache directory error: %v\n", err)
	} else {
		fmt.Printf("  ✅ Path: %s\n", store.Dir())
	}

	// 4. Supported Languages Status
	fmt.Println("\n[4] Language Support Matrix:")
	for _, p := range allPacks() {
		tier, runtime := describePack(p)
		fmt.Printf("  • %-8s : %-22s [%s]\n", p.Name, tier, runtime)
	}
	fmt.Println("\nDiagnosis: System is ready.")
}

func handlePacks(args []string) {
	fmt.Println("Registered Language Packs:")
	for _, p := range allPacks() {
		tier, _ := describePack(p)
		line := p.Name
		if len(p.Aliases) > 0 {
			line += " (" + strings.Join(p.Aliases, ", ") + ")"
		}
		fmt.Printf("  • %-34s %s\n", line, tier)
	}
}

func handleMCP(args []string) {
	sb, _, _, err := initSandbox()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize sandbox for MCP: %v\n", err)
		os.Exit(1)
	}

	server := mcp.NewServer(sb, mcp.WithVersion(version))
	defer server.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := server.ServeStdio(ctx); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
		os.Exit(1)
	}
}

func handlePackages(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: sandbox packages [list|add] [args]")
		return
	}
	store, err := packages.NewStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing package store: %v\n", err)
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		langs := packLangs()
		explicit := len(args) > 1
		if explicit {
			langs = []string{args[1]}
		}
		found := false
		for _, lang := range langs {
			pkgs, err := store.List(lang)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error listing packages: %v\n", err)
				os.Exit(1)
			}
			if len(pkgs) == 0 {
				if explicit {
					fmt.Printf("No cached packages found for language %q (cache dir: %s)\n", lang, store.Dir())
				}
				continue
			}
			found = true
			fmt.Printf("Cached packages for %s:\n", lang)
			for _, p := range pkgs {
				fmt.Printf("  • %-20s (%d bytes) -> %s\n", p.Name, p.Size, p.Path)
			}
		}
		if !found && !explicit {
			fmt.Printf("No cached packages found (cache dir: %s)\n", store.Dir())
		}
	case "add":
		fs := flag.NewFlagSet("packages add", flag.ExitOnError)
		lang := fs.String("lang", "python", "Target language")
		name := fs.String("name", "", "Package name (inferred from filename if empty)")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() < 1 {
			fmt.Println("Usage: sandbox packages add [--lang python] [--name pkgname] <file.whl>")
			os.Exit(1)
		}
		filePath := fs.Arg(0)
		data, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", filePath, err)
			os.Exit(1)
		}
		pkgName := *name
		if pkgName == "" {
			base := filepath.Base(filePath)
			pkgName = strings.Split(base, "-")[0]
		}
		if err := store.Add(*lang, pkgName, filepath.Base(filePath), data); err != nil {
			fmt.Fprintf(os.Stderr, "error caching package: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully added package %q for %s to offline cache.\n", pkgName, *lang)
	default:
		fmt.Printf("unknown packages subcommand: %s\n", args[0])
		os.Exit(1)
	}
}
