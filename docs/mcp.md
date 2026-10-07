# Model Context Protocol (MCP) Server

`github.com/micromax/sandbox` includes a built-in **Model Context Protocol (MCP)** server, turning `sandbox` into an out-of-the-box, secure code interpreter tool for AI assistants such as **Claude Desktop**, **Cursor**, **Zed**, and custom LLM agents.

---

## 1. Quickstart: Claude Desktop Integration

Claude Desktop can run `sandbox mcp` over standard I/O (stdio) to safely execute Python, TypeScript, JavaScript, Lua, and more on your machine with **zero Docker daemon required**.

### Configuration

Edit your Claude Desktop configuration file:
* **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
* **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
* **Linux**: `~/.config/Claude/claude_desktop_config.json`

Add the `sandbox` server:

```json
{
  "mcpServers": {
    "sandbox": {
      "command": "sandbox",
      "args": ["mcp"]
    }
  }
}
```

> **Tip**: If `sandbox` is not in your global system `PATH`, specify the full binary path (e.g. `C:\\Users\\<username>\\go\\bin\\sandbox.exe` on Windows or `/usr/local/bin/sandbox` on macOS/Linux).

Restart Claude Desktop. You will see the hammer icon 🔨 indicating that the following tools are now active:
* `execute_code`: One-shot stateless code execution with memory & timeout caps.
* `eval_session`: Multi-turn stateful REPL execution (preserves variables across conversation turns).
* `list_languages`: Discover available language packs and execution tiers.

---

## 2. Cursor IDE Integration

In **Cursor Settings > Features > MCP**, click **Add New MCP Server**:
* **Name**: `sandbox`
* **Type**: `command`
* **Command**: `sandbox mcp`

---

## 3. Available Tools

### 1. `execute_code`
Executes code in a strict, isolated environment.

**Parameters**:
| Parameter | Type | Required | Description |
|---|---|---|---|
| `lang` | `string` | **Yes** | Language: `ts`, `js`, `python`, `lua`, `wasm`, `go`, `rust`, `java`, `bash` |
| `code` | `string` | **Yes** | Source code to execute |
| `stdin` | `string` | No | Optional standard input string |
| `timeout_seconds` | `number` | No | Wall-clock execution deadline (default: 10s, max: 60s) |
| `memory_limit_mb` | `number` | No | Memory limit in megabytes |

**Example Tool Call**:
```json
{
  "name": "execute_code",
  "arguments": {
    "lang": "ts",
    "code": "interface Stat { mean: number }; const s: Stat = { mean: 42 }; console.log('Mean:', s.mean);"
  }
}
```

---

### 2. `eval_session`
Evaluates code in an interactive, persistent REPL session. Variables, imports, and function definitions are preserved across calls within the same `session_id`.

**Parameters**:
| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | `string` | **Yes** | Conversation or notebook session identifier (e.g. `chat-123`) |
| `lang` | `string` | **Yes** | Language: `ts`, `js`, `python`, `lua` |
| `code` | `string` | **Yes** | Expression or statement to evaluate |

**Example Sequence**:
1. First Turn:
   ```json
   { "session_id": "math-session", "lang": "python", "code": "import math; base = 10" }
   ```
2. Second Turn:
   ```json
   { "session_id": "math-session", "lang": "python", "code": "math.pow(base, 3)" }
   ```
   *(Output: `1000.0`)*

---

### 3. `list_languages`
Returns an overview of all installed language packs, default backends (Pure WebAssembly vs Docker), and memory floors.

---

## 4. Using in Go AI Agents (OpenAI & Anthropic SDKs)

If you are building your own AI agent in Go, the `github.com/micromax/sandbox/mcp` package directly exports function schemas:

```go
package main

import (
	"fmt"

	"github.com/micromax/sandbox/mcp"
)

func main() {
	// Ready-to-use OpenAI function definitions
	openAITools := mcp.OpenAITools()
	fmt.Printf("OpenAI tools registered: %d\n", len(openAITools))

	// Ready-to-use Anthropic Claude tool definitions
	anthropicTools := mcp.AnthropicTools()
	fmt.Printf("Anthropic tools registered: %d\n", len(anthropicTools))
}
```

---

## 5. Security & Isolation

When AI models call the sandbox via MCP:
1. **Tier 1 Languages (TypeScript, JavaScript, Python, Lua, Raw WASI)** run **inside WebAssembly** with zero network access (deny-by-default) and no access to your host filesystem.
2. **Crash & Infinite Loop Resilience**: CPU deadlines and memory page limits kill runaway model code instantly without affecting the host process or client application.
3. **No Docker Daemon Required**: Pure-Go runtime means you can ship and run AI code execution on any machine with zero container configuration.
