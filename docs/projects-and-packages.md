# Running Multi-File Projects & Package Dependencies

In real-world applications, untrusted or AI-generated scripts rarely run in isolation as a single script. They frequently depend on:
1. **Local project files**: Project submodules, shared utilities, domain models, or configuration files (e.g. `from services.calc import add`, `from config import SETTINGS`).
2. **Third-party packages**: External library dependencies (e.g. `sqlparse`, `six`, `requests`, `pydantic`).

`micromax/sandbox` provides a unified, secure system to execute multi-file project codebases and third-party packages inside an **air-gapped, isolated environment** with zero risk to the host machine.

---

## Architecture & How It Works

```
Host Machine (Isolated & Read-Only)
┌────────────────────────────────────────────────────────┐
│  Host Project Directory                                │
│  ├── config.py                                         │
│  ├── services/                                         │
│  │   └── analyzer.py                                   │
│  └── main.py                                           │
└───────────────────────────┬────────────────────────────┘
                            │ sandbox.LoadProjectFiles()
                            │ (skips .git, .venv, node_modules)
                            ▼
┌────────────────────────────────────────────────────────┐
│  Sandbox Isolated Guest (WebAssembly / Container)      │
│                                                        │
│  /in (Read-Only VFS)                                   │
│  ├── config.py                                         │
│  ├── services/analyzer.py                              │
│  └── site-packages/ (Injected from Local Cache Store)  │
│      └── sqlparse/                                     │
│                                                        │
│  Environment Search Path:                              │
│  PYTHONPATH=/lib/python3.13:/in:/in/site-packages      │
│                                                        │
│  /work (Isolated read-write scratch space)             │
│  /out  (Optional captured artifacts)                   │
└────────────────────────────────────────────────────────┘
```

### Key Security Guarantees:
1. **Read-Only Project Mount**: All project code and packages are mounted in `/in`. Guest code can never modify, overwrite, or delete host source files.
2. **Automated Ignore Filter**: Sensitive directories (`.git`, `.env`, `.venv`, `node_modules`, `__pycache__`, `.vscode`) are automatically ignored during project loading.
3. **No Unchecked Host Traversal**: The guest cannot traverse out of `/in`. Any attempt to access `/etc/passwd`, host drives, or parent folders (`../../`) is blocked by the virtual filesystem.
4. **Air-Gapped Package Injection**: Third-party packages are sourced from a local offline package cache—no outbound network connection or `pip install` required at runtime.

---

## 1. Managing Offline Packages

`micromax/sandbox` includes an offline package store located on the host machine at:
- **Windows**: `%LOCALAPPDATA%\micromax-sandbox\packages\<lang>\`
- **Linux / macOS**: `~/.cache/micromax-sandbox/packages/<lang>/`

### CLI Package Management

#### List Installed Packages
```bash
sandbox packages list
```
Output:
```text
LANGUAGE   PACKAGE      FILE
python     six          six-1.17.0-py2.py3-none-any.whl
python     sqlparse     sqlparse-0.6.0-py3-none-any.whl
```

#### Add a Package to Cache
To add a package wheel to the offline store:
```bash
sandbox packages add python /path/to/sqlparse-0.6.0-py3-none-any.whl
```

> **Tier Compatibility Note**:
> * **Pure Python Wheels** (`py3-none-any.whl` like `six`, `sqlparse`, `jinja2`) run directly in the **Tier 1 Pure WebAssembly** runtime (zero Docker, instant cold-starts).
> * **Compiled C-Extensions** (packages requiring `.so` or `.pyd` binaries like `numpy` or `cryptography`) should be executed on the **Tier 2 Docker** backend.

---

## 2. Running via the Command-Line Interface (CLI)

### Single-Command Project Execution: `--dir` & `--pkg`

Use the `--dir` flag to point to a local codebase, and `--pkg` to request cached packages:

```bash
sandbox run --dir ./examples/multi-file-project --pkg sqlparse ./examples/multi-file-project/main.py
```

### Flags for Project Execution:
| Flag | Description |
|---|---|
| `--dir <path>` | Root directory of the local project to scan and inject into `/in`. |
| `--pkg <list>` | Comma-separated list of packages to inject from cache (e.g. `sqlparse,six`). |
| `--mem <size>` | Memory ceiling (e.g. `128M`, `256M`). |
| `--timeout <dur>` | Maximum execution duration (e.g. `10s`, `1m`). |
| `--json` | Output execution outcome, duration, and memory usage as structured JSON. |

---

## 3. Running via Model Context Protocol (MCP)

For AI agents, IDE extensions, and Claude Desktop, the MCP tool `execute_code` supports project dependencies through two parameters: `project_dir` and `files`.

### Option A: Local Host Project Directory (`project_dir`)
When running locally with tool access to the filesystem:

```json
{
  "name": "execute_code",
  "arguments": {
    "lang": "python",
    "project_dir": "c:/Users/hp/sandbox/examples/multi-file-project",
    "packages": ["sqlparse"],
    "code": "import main"
  }
}
```

### Option B: In-Memory Multi-File Injection (`files`)
When an AI agent generates multiple files or operates remotely without filesystem access:

```json
{
  "name": "execute_code",
  "arguments": {
    "lang": "python",
    "packages": ["sqlparse"],
    "files": {
      "config.py": "APP_NAME = 'PrimaryCare Engine'\nVERSION = '2.4.0'\n",
      "services/analyzer.py": "import sqlparse\nfrom config import APP_NAME\ndef analyze(q):\n    return f'[{APP_NAME}] Formatted: ' + sqlparse.format(q, reindent=True)\n"
    },
    "code": "from services.analyzer import analyze\nprint(analyze('select * from users;'))"
  }
}
```

---

## 4. Running via the Go SDK (`github.com/micromax/sandbox`)

In Go applications, you can use `sandbox.LoadProjectFiles` to load project directories directly into `sandbox.Spec`:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/micromax/sandbox"
	backendwasm "github.com/micromax/sandbox/backend/wasm"
	packpython "github.com/micromax/sandbox/packs/python"
)

func main() {
	// 1. Initialize the Wasm backend
	wasmBackend, err := backendwasm.New()
	if err != nil {
		panic(err)
	}

	// 2. Initialize Sandbox
	sb, err := sandbox.New(
		sandbox.WithBackends(wasmBackend),
		sandbox.WithPacks(packpython.Pack()),
	)
	if err != nil {
		panic(err)
	}
	defer sb.Close()

	// 3. Scan and collect project files safely
	projectFiles, err := sandbox.LoadProjectFiles("./examples/multi-file-project")
	if err != nil {
		panic(err)
	}

	// 4. Execute within sandbox
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := sb.Run(ctx, sandbox.Spec{
		Lang:     "python",
		Code:     "import main",
		Files:    projectFiles,
		Packages: []string{"sqlparse"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Exit Code:", result.ExitCode)
	fmt.Println("Output:\n" + string(result.Stdout))
}
```

---

## 5. Walkthrough: Verified Sample Application

The repository includes a complete working example under [`examples/multi-file-project/`](file:///c:/Users/hp/sandbox/examples/multi-file-project).

### Structure:
```
examples/multi-file-project/
├── config.py               # Shared configuration constants
├── services/
│   └── analyzer.py         # Submodule importing config and 3rd-party sqlparse
└── main.py                 # Application entrypoint
```

#### `config.py`
```python
APP_NAME = "PrimaryCare Engine"
VERSION = "2.4.0"
DEBUG = False
```

#### `services/analyzer.py`
```python
import sqlparse
from config import APP_NAME, VERSION

def analyze_query(sql_text: str) -> dict:
    parsed = sqlparse.parse(sql_text)
    formatted = sqlparse.format(sql_text, reindent=True, keyword_case='upper')
    return {
        "engine": f"{APP_NAME} v{VERSION}",
        "statement_count": len(parsed),
        "formatted": formatted,
    }
```

#### `main.py`
```python
import json
from services.analyzer import analyze_query

raw_query = "select id, name, email from users where active = 1 order by id desc;"
result = analyze_query(raw_query)

print("=== MULTI-FILE PROJECT EXECUTION SUCCESS ===")
print(f"Engine: {result['engine']}")
print(f"Statements detected: {result['statement_count']}")
print("Formatted SQL:")
print(result['formatted'])
```

### Running the Example:
```bash
sandbox run --dir ./examples/multi-file-project --pkg sqlparse ./examples/multi-file-project/main.py
```

### Expected Output:
```text
=== MULTI-FILE PROJECT EXECUTION SUCCESS ===
Engine: PrimaryCare Engine v2.4.0
Statements detected: 1
Formatted SQL:
SELECT id,
       name,
       email
FROM users
WHERE active = 1
ORDER BY id DESC;
```
