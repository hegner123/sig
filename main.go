package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// Extractor is the interface each language implements to extract API surface.
type Extractor interface {
	// Extensions returns the file extensions this extractor handles (e.g. ".go", ".rs").
	Extensions() []string
	// Extract parses the file and returns its shape.
	// When exportedOnly is true, only public/exported symbols are included.
	Extract(filePath string, exportedOnly bool) (*FileShape, error)
}

// Registry

var extractors = map[string]Extractor{}

func registerExtractor(e Extractor) {
	for _, ext := range e.Extensions() {
		extractors[ext] = e
	}
}

func supportedExtensions() []string {
	exts := make([]string, 0, len(extractors))
	for ext := range extractors {
		exts = append(exts, ext)
	}
	return exts
}

func init() {
	registerExtractor(&GoExtractor{})
}

func extractShape(filePath string, exportedOnly bool) (*FileShape, error) {
	ext := filepath.Ext(filePath)
	e, ok := extractors[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported file type %q (supported: %s)", ext, strings.Join(supportedExtensions(), ", "))
	}
	return e.Extract(filePath, exportedOnly)
}

// Output types

type FileShape struct {
	File      string     `json:"file"`
	Package   string     `json:"package"`
	Imports   []string   `json:"imports,omitempty"`
	Types     []TypeDef  `json:"types,omitempty"`
	Functions []FuncDef  `json:"functions,omitempty"`
	Constants []ValueDef `json:"constants,omitempty"`
	Variables []ValueDef `json:"variables,omitempty"`
}

type TypeDef struct {
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Line       int        `json:"line"`
	Fields     []FieldDef `json:"fields,omitempty"`
	Methods    []FuncDef  `json:"methods,omitempty"`
	Embeds     []string   `json:"embeds,omitempty"`
	Underlying string     `json:"underlying,omitempty"`
}

type FieldDef struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type"`
	Tag  string `json:"tag,omitempty"`
}

type FuncDef struct {
	Name      string `json:"name"`
	Receiver  string `json:"receiver,omitempty"`
	Signature string `json:"signature"`
	Line      int    `json:"line"`
}

type ValueDef struct {
	Name  string `json:"name"`
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
	Line  int    `json:"line"`
}

// MCP JSON-RPC types

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Capabilities    Capabilities `json:"capabilities"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capabilities struct {
	Tools map[string]bool `json:"tools"`
}

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     any    `json:"default,omitempty"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolCallResult struct {
	Content []ContentItem `json:"content"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Entry point

func main() {
	cli := flag.Bool("cli", false, "Run in CLI mode (default is MCP server mode)")
	all := flag.Bool("all", false, "Include unexported/private symbols")
	flag.Parse()

	if *cli {
		runCLI(*all, flag.Args())
		return
	}

	runMCPServer()
}

const (
	exitSuccess = 0
	exitError   = 1
)

func runCLI(all bool, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: sig --cli [--all] <file>")
		fmt.Fprintf(os.Stderr, "Supported extensions: %s\n", strings.Join(supportedExtensions(), ", "))
		os.Exit(exitError)
	}

	shape, err := extractShape(args[0], !all)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitError)
	}

	output, err := json.MarshalIndent(shape, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling result: %v\n", err)
		os.Exit(exitError)
	}

	fmt.Println(string(output))
}

// MCP server

func runMCPServer() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	scanner := bufio.NewScanner(os.Stdin)
	lineChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		for scanner.Scan() {
			lineChan <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			errChan <- err
		}
		close(lineChan)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errChan:
			fmt.Fprintf(os.Stderr, "Scanner error: %v\n", err)
			return
		case line, ok := <-lineChan:
			if !ok {
				return
			}
			if line == "" {
				continue
			}

			var req JSONRPCRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				sendError(nil, -32700, "Parse error")
				continue
			}

			handleRequest(req)
		}
	}
}

func handleRequest(req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		handleInitialize(req)
	case "notifications/initialized":
		return
	case "tools/list":
		handleToolsList(req)
	case "tools/call":
		handleToolsCall(req)
	default:
		if req.ID != nil {
			sendError(req.ID, -32601, "Method not found")
		}
	}
}

func handleInitialize(req JSONRPCRequest) {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo: ServerInfo{
			Name:    "sig",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Tools: map[string]bool{
				"list": true,
				"call": true,
			},
		},
	}
	sendResponse(req.ID, result)
}

func handleToolsList(req JSONRPCRequest) {
	desc := fmt.Sprintf(
		"Extract the public API surface from a source file. Returns function signatures, type/struct/interface definitions, const/var blocks as compact JSON without implementation bodies. Supported extensions: %s",
		strings.Join(supportedExtensions(), ", "),
	)

	result := ToolsListResult{
		Tools: []Tool{
			{
				Name:        "sig",
				Description: desc,
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"file": {
							Type:        "string",
							Description: "Absolute path to the source file to analyze.",
						},
						"all": {
							Type:        "boolean",
							Description: "Include unexported/private symbols. Defaults to false (public only).",
							Default:     false,
						},
					},
					Required: []string{"file"},
				},
			},
		},
	}
	sendResponse(req.ID, result)
}

func handleToolsCall(req JSONRPCRequest) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		sendError(req.ID, -32602, "Invalid params")
		return
	}

	if params.Name != "sig" {
		sendError(req.ID, -32602, "Unknown tool")
		return
	}

	filePath, ok := params.Arguments["file"].(string)
	if !ok || filePath == "" {
		sendError(req.ID, -32602, "Missing or invalid 'file' parameter")
		return
	}

	exportedOnly := true
	if all, ok := params.Arguments["all"].(bool); ok && all {
		exportedOnly = false
	}

	shape, err := extractShape(filePath, exportedOnly)
	if err != nil {
		sendError(req.ID, -32603, fmt.Sprintf("Extract failed: %v", err))
		return
	}

	jsonResult, err := json.Marshal(shape)
	if err != nil {
		sendError(req.ID, -32603, "Failed to marshal result")
		return
	}

	response := ToolCallResult{
		Content: []ContentItem{
			{Type: "text", Text: string(jsonResult)},
		},
	}
	sendResponse(req.ID, response)
}

func sendResponse(id any, result any) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal response: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

func sendError(id any, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal error: %v\n", err)
		return
	}
	fmt.Println(string(data))
}
