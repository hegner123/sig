# sig

Extract the public API surface from source files as compact JSON — function signatures, type/struct/interface definitions, const/var blocks — without implementation bodies.

## Supported Languages

| Extension | Language |
|-----------|----------|
| `.go` | Go |
| `.ts`, `.tsx`, `.mts`, `.cts` | TypeScript |
| `.cs` | C# |

## Installation

```bash
go build -o sig
codesign -s - sig
sudo cp sig /usr/local/bin/
sudo codesign -f -s - /usr/local/bin/sig
```

Or with [just](https://github.com/casey/just):

```bash
just install
```

## Usage

### CLI

```bash
sig --cli file.go              # public API only
sig --cli --all file.go        # include private/unexported symbols
```

### MCP Server

`sig` runs as an MCP JSON-RPC stdio server by default (no flags). Register it with Claude Code:

```bash
claude mcp add --transport stdio --scope user sig -- /usr/local/bin/sig
```

**Tool:** `sig`
**Parameters:**
- `file` (string, required) — absolute path to source file
- `all` (boolean, optional) — include private/unexported symbols (default: false)

## Output

Returns a `FileShape` JSON object:

```json
{
  "file": "/path/to/file.go",
  "package": "main",
  "imports": ["fmt", "os"],
  "types": [
    {
      "name": "Server",
      "kind": "struct",
      "line": 15,
      "fields": [{"name": "Port", "type": "int"}],
      "methods": [{"name": "Start", "signature": "() error", "line": 20}]
    }
  ],
  "functions": [
    {"name": "NewServer", "signature": "(port int) *Server", "line": 10}
  ],
  "constants": [],
  "variables": []
}
```

## License

MIT
