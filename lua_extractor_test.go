package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempLua(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lua")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestLuaExtensions(t *testing.T) {
	e := &LuaExtractor{}
	exts := e.Extensions()
	if len(exts) != 1 || exts[0] != ".lua" {
		t.Errorf("expected [.lua], got %v", exts)
	}
}

func TestLuaPackageName(t *testing.T) {
	path := writeTempLua(t, "-- empty file\n")
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if shape.Package != "test" {
		t.Errorf("expected package 'test', got %q", shape.Package)
	}
}

func TestLuaRequireImports(t *testing.T) {
	src := `local json = require("cjson")
local socket = require 'socket'
require("global_setup")
local http = require( "http.client" )
M = require("module")
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"json cjson",
		"socket socket",
		"global_setup",
		"http http.client",
		"M module",
	}

	if len(shape.Imports) != len(expected) {
		t.Fatalf("expected %d imports, got %d: %v", len(expected), len(shape.Imports), shape.Imports)
	}
	for i, want := range expected {
		if shape.Imports[i] != want {
			t.Errorf("import[%d]: expected %q, got %q", i, want, shape.Imports[i])
		}
	}
}

func TestLuaGlobalFunctions(t *testing.T) {
	src := `function foo(a, b, c)
    return a + b + c
end

function bar()
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(shape.Functions))
	}

	f := shape.Functions[0]
	if f.Name != "foo" || f.Signature != "(a, b, c)" || f.Receiver != "" {
		t.Errorf("foo: got name=%q sig=%q recv=%q", f.Name, f.Signature, f.Receiver)
	}

	f = shape.Functions[1]
	if f.Name != "bar" || f.Signature != "()" {
		t.Errorf("bar: got name=%q sig=%q", f.Name, f.Signature)
	}
}

func TestLuaModuleFunctions(t *testing.T) {
	src := `local M = {}

function M.new(config)
    return config
end

function M:connect(host, port)
    return true
end

function M.sub.deep(x)
    return x
end

return M
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(shape.Functions) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(shape.Functions))
	}

	tests := []struct {
		name, recv, sig string
	}{
		{"new", "M", "(config)"},
		{"connect", "M", "(self, host, port)"},
		{"deep", "M.sub", "(x)"},
	}

	for i, tt := range tests {
		f := shape.Functions[i]
		if f.Name != tt.name || f.Receiver != tt.recv || f.Signature != tt.sig {
			t.Errorf("func[%d]: expected %s/%s/%s, got %s/%s/%s",
				i, tt.name, tt.recv, tt.sig, f.Name, f.Receiver, f.Signature)
		}
	}
}

func TestLuaAssignedFunctions(t *testing.T) {
	src := `local M = {}

M.handler = function(req, resp)
    return resp
end

return M
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(shape.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(shape.Functions))
	}
	f := shape.Functions[0]
	if f.Name != "handler" || f.Receiver != "M" || f.Signature != "(req, resp)" {
		t.Errorf("got name=%q recv=%q sig=%q", f.Name, f.Receiver, f.Signature)
	}
}

func TestLuaLocalFunctions(t *testing.T) {
	src := `local function helper(x)
    return x * 2
end

local cb = function(event)
    print(event)
end

function public_func()
end
`
	path := writeTempLua(t, src)

	// Exported only: should only see public_func
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 {
		t.Fatalf("exported only: expected 1 function, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if shape.Functions[0].Name != "public_func" {
		t.Errorf("expected public_func, got %q", shape.Functions[0].Name)
	}

	// All symbols: should see all 3
	shape, err = (&LuaExtractor{}).Extract(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 3 {
		t.Fatalf("all: expected 3 functions, got %d", len(shape.Functions))
	}
	names := []string{"helper", "cb", "public_func"}
	for i, want := range names {
		if shape.Functions[i].Name != want {
			t.Errorf("func[%d]: expected %q, got %q", i, want, shape.Functions[i].Name)
		}
	}
}

func TestLuaVariables(t *testing.T) {
	src := `local M = {}

M.VERSION = "1.0.0"
M.MAX_RETRIES = 3
M.DEBUG = false
M.NIL_VAL = nil

local secret = "hidden"

return M
`
	path := writeTempLua(t, src)

	// Exported only
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(shape.Variables) != 4 {
		t.Fatalf("expected 4 variables, got %d: %v", len(shape.Variables), shape.Variables)
	}

	tests := []struct {
		name, value string
	}{
		{"M.VERSION", `"1.0.0"`},
		{"M.MAX_RETRIES", "3"},
		{"M.DEBUG", "false"},
		{"M.NIL_VAL", "nil"},
	}
	for i, tt := range tests {
		v := shape.Variables[i]
		if v.Name != tt.name || v.Value != tt.value {
			t.Errorf("var[%d]: expected %s=%s, got %s=%s", i, tt.name, tt.value, v.Name, v.Value)
		}
	}

	// All symbols: should also include secret and M
	shape, err = (&LuaExtractor{}).Extract(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Variables) != 6 {
		t.Fatalf("all: expected 6 variables, got %d: %v", len(shape.Variables), shape.Variables)
	}
}

func TestLuaVarargs(t *testing.T) {
	src := `function fmt(pattern, ...)
    return string.format(pattern, ...)
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(shape.Functions))
	}
	if shape.Functions[0].Signature != "(pattern, ...)" {
		t.Errorf("expected (pattern, ...), got %q", shape.Functions[0].Signature)
	}
}

func TestLuaColonMethodEmptyParams(t *testing.T) {
	src := `local M = {}
function M:destroy()
    self.conn = nil
end
return M
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(shape.Functions))
	}
	f := shape.Functions[0]
	if f.Signature != "(self)" {
		t.Errorf("expected (self), got %q", f.Signature)
	}
	if f.Receiver != "M" {
		t.Errorf("expected receiver M, got %q", f.Receiver)
	}
}

func TestLuaBlockComments(t *testing.T) {
	src := `--[[ This is a
multi-line block comment ]]

--[==[
  Another block comment
  with different level
]==]

function after_comments(x)
    return x
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "after_comments" {
		t.Errorf("expected after_comments, got %v", shape.Functions)
	}
}

func TestLuaLongStrings(t *testing.T) {
	src := `local M = {}

local template = [[
function fake_function()
    -- this is inside a long string, should NOT be extracted
end
]]

local other = [==[
M.fake = "not real"
]==]

function M.real(x)
    return x
end

return M
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "real" {
		t.Errorf("expected only 'real' function, got %v", shape.Functions)
	}
}

func TestLuaNestedDepthTracking(t *testing.T) {
	src := `function outer(a)
    if a > 0 then
        for i = 1, a do
            repeat
                a = a - 1
            until a == 0
        end
    else
        while a < 0 do
            a = a + 1
        end
    end
    do
        local temp = a
    end
    return a
end

function after_outer()
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if shape.Functions[0].Name != "outer" || shape.Functions[1].Name != "after_outer" {
		t.Errorf("expected outer, after_outer - got %s, %s",
			shape.Functions[0].Name, shape.Functions[1].Name)
	}
}

func TestLuaTableConstructorIsolation(t *testing.T) {
	src := `local config = {
    handler = function(req)
        return req
    end,
    name = "test",
    nested = {
        deep = function() end,
    },
}

function real_func()
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	// Only real_func should be extracted (table innards are not top-level)
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "real_func" {
		t.Errorf("expected only real_func, got %v", shape.Functions)
	}
}

func TestLuaMultilineParams(t *testing.T) {
	src := `function multiline(
    first,
    second, -- a comment
    third
)
    return first + second + third
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 {
		t.Fatal("expected 1 function")
	}
	if shape.Functions[0].Signature != "(first, second, third)" {
		t.Errorf("expected (first, second, third), got %q", shape.Functions[0].Signature)
	}
}

func TestLuaEqualityNotAssignment(t *testing.T) {
	src := `if x == 5 then
    print("nope")
end

function real()
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	// Should not extract x as a variable (== is comparison, not assignment)
	if len(shape.Variables) != 0 {
		t.Errorf("expected 0 variables, got %d: %v", len(shape.Variables), shape.Variables)
	}
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "real" {
		t.Errorf("expected real function, got %v", shape.Functions)
	}
}

func TestLuaEmptyFile(t *testing.T) {
	path := writeTempLua(t, "")
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 0 && len(shape.Variables) != 0 && len(shape.Imports) != 0 {
		t.Errorf("expected empty shape for empty file")
	}
}

func TestLuaCommentOnlyFile(t *testing.T) {
	src := `-- Just comments
-- Nothing else
--[[ block comment ]]
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(shape.Functions))
	}
}

func TestLuaStringEscapes(t *testing.T) {
	src := `M = {}
M.PATTERN = "function end if"
M.ESCAPED = "has \"quotes\" inside"
M.SINGLE = 'single\'s quote'

function M.real()
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	// Strings with keywords inside should not confuse the parser
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "real" {
		t.Errorf("expected 1 function 'real', got %v", shape.Functions)
	}
}

func TestLuaAnonymousFunctionAtTopLevel(t *testing.T) {
	src := `print(function()
    return 1
end)

function after()
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	// Anonymous function should not be extracted; after should be
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "after" {
		t.Errorf("expected only 'after', got %v", shape.Functions)
	}
}

func TestLuaLocalMultipleNames(t *testing.T) {
	src := `local a, b, c
local x, y = 1, 2
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Variables) != 5 {
		t.Fatalf("expected 5 variables, got %d: %v", len(shape.Variables), shape.Variables)
	}
	names := []string{"a", "b", "c", "x", "y"}
	for i, want := range names {
		if shape.Variables[i].Name != want {
			t.Errorf("var[%d]: expected %q, got %q", i, want, shape.Variables[i].Name)
		}
	}
}

func TestLuaNestedFunctionDepth(t *testing.T) {
	src := `function outer()
    local function inner()
        local function deep()
            return 1
        end
        return deep()
    end
    return inner()
end

function second()
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if shape.Functions[0].Name != "outer" || shape.Functions[1].Name != "second" {
		t.Errorf("expected outer/second, got %s/%s",
			shape.Functions[0].Name, shape.Functions[1].Name)
	}
}

func TestLuaLineNumbers(t *testing.T) {
	src := `-- line 1
-- line 2
function first() -- line 3
end

-- line 6
function second() -- line 7
end
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(shape.Functions))
	}
	if shape.Functions[0].Line != 3 {
		t.Errorf("first: expected line 3, got %d", shape.Functions[0].Line)
	}
	if shape.Functions[1].Line != 7 {
		t.Errorf("second: expected line 7, got %d", shape.Functions[1].Line)
	}
}

func TestLuaFunctionCallNotVariable(t *testing.T) {
	src := `print("hello")
io.write("world")
M = {}
M.init()
`
	path := writeTempLua(t, src)
	shape, err := (&LuaExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	// Only M = {} should be captured as a variable, not print/io.write/M.init
	if len(shape.Variables) != 1 || shape.Variables[0].Name != "M" {
		t.Errorf("expected only M variable, got %v", shape.Variables)
	}
}
