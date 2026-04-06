package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempPy(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.py")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func writeTempPyi(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pyi")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestPyExtensions(t *testing.T) {
	e := &PyExtractor{}
	exts := e.Extensions()
	if len(exts) != 2 {
		t.Fatalf("expected 2 extensions, got %d: %v", len(exts), exts)
	}
	found := map[string]bool{}
	for _, ext := range exts {
		found[ext] = true
	}
	if !found[".py"] || !found[".pyi"] {
		t.Errorf("expected .py and .pyi, got %v", exts)
	}
}

func TestPyPackageName(t *testing.T) {
	path := writeTempPy(t, "# empty\n")
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if shape.Package != "test" {
		t.Errorf("expected package 'test', got %q", shape.Package)
	}
}

func TestPyPackageNamePyi(t *testing.T) {
	path := writeTempPyi(t, "# stub\n")
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if shape.Package != "test" {
		t.Errorf("expected package 'test', got %q", shape.Package)
	}
}

func TestPyImportSimple(t *testing.T) {
	src := `import os
import sys
import json
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"os", "sys", "json"}
	if len(shape.Imports) != len(expected) {
		t.Fatalf("expected %d imports, got %d: %v", len(expected), len(shape.Imports), shape.Imports)
	}
	for i, want := range expected {
		if shape.Imports[i] != want {
			t.Errorf("import[%d]: expected %q, got %q", i, want, shape.Imports[i])
		}
	}
}

func TestPyImportAlias(t *testing.T) {
	src := `import numpy as np
import pandas as pd
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"np numpy", "pd pandas"}
	if len(shape.Imports) != len(expected) {
		t.Fatalf("expected %d imports, got %d: %v", len(expected), len(shape.Imports), shape.Imports)
	}
	for i, want := range expected {
		if shape.Imports[i] != want {
			t.Errorf("import[%d]: expected %q, got %q", i, want, shape.Imports[i])
		}
	}
}

func TestPyFromImport(t *testing.T) {
	src := `from os import path
from collections import OrderedDict
from os.path import join, exists
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"os.path",
		"collections.OrderedDict",
		"os.path.join",
		"os.path.exists",
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

func TestPyFromImportAlias(t *testing.T) {
	src := `from foo import bar as baz
from os.path import join as pjoin
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"baz foo.bar",
		"pjoin os.path.join",
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

func TestPyFromImportRelative(t *testing.T) {
	src := `from . import utils
from .. import base
from .models import User
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"..utils",
		"...base",
		".models.User",
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

func TestPyFromImportParenthesized(t *testing.T) {
	src := `from os.path import (
    join,
    exists,
    dirname,  # trailing comma
)
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"os.path.join",
		"os.path.exists",
		"os.path.dirname",
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

func TestPyFromImportStar(t *testing.T) {
	src := `from os.path import *
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Imports) != 1 || shape.Imports[0] != "os.path.*" {
		t.Errorf("expected [os.path.*], got %v", shape.Imports)
	}
}

func TestPyModuleFunctions(t *testing.T) {
	src := `def foo(a: int, b: str) -> bool:
    return True

def bar():
    pass

def baz(x, y=10, *args, **kwargs):
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 3 {
		t.Fatalf("expected 3 functions, got %d: %v", len(shape.Functions), shape.Functions)
	}

	tests := []struct {
		name string
		sig  string
	}{
		{"foo", "(a: int, b: str) -> bool"},
		{"bar", "()"},
		{"baz", "(x, y=10, *args, **kwargs)"},
	}
	for i, tt := range tests {
		f := shape.Functions[i]
		if f.Name != tt.name {
			t.Errorf("func[%d]: expected name %q, got %q", i, tt.name, f.Name)
		}
		if f.Signature != tt.sig {
			t.Errorf("func[%d] %q: expected sig %q, got %q", i, tt.name, tt.sig, f.Signature)
		}
	}
}

func TestPyAsyncFunction(t *testing.T) {
	src := `async def fetch(url: str) -> bytes:
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(shape.Functions))
	}
	f := shape.Functions[0]
	if f.Name != "fetch" {
		t.Errorf("expected name 'fetch', got %q", f.Name)
	}
	if f.Signature != "async (url: str) -> bytes" {
		t.Errorf("expected 'async (url: str) -> bytes', got %q", f.Signature)
	}
}

func TestPyDecoratedFunction(t *testing.T) {
	src := `@staticmethod
def create(name: str) -> None:
    pass

@cache
def expensive(n: int) -> int:
    return n * n
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d: %v", len(shape.Functions), shape.Functions)
	}

	f := shape.Functions[0]
	if f.Name != "create" {
		t.Errorf("expected name 'create', got %q", f.Name)
	}
	if f.Signature != "@staticmethod (name: str) -> None" {
		t.Errorf("expected '@staticmethod (name: str) -> None', got %q", f.Signature)
	}

	f = shape.Functions[1]
	if f.Name != "expensive" {
		t.Errorf("expected name 'expensive', got %q", f.Name)
	}
	if f.Signature != "@cache (n: int) -> int" {
		t.Errorf("expected '@cache (n: int) -> int', got %q", f.Signature)
	}
}

func TestPyMultilineSignature(t *testing.T) {
	src := `def multiline(
    a: int,
    b: str,  # a comment
    c: float = 3.14
) -> bool:
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(shape.Functions))
	}
	expected := "(a: int, b: str, c: float = 3.14) -> bool"
	if shape.Functions[0].Signature != expected {
		t.Errorf("expected %q, got %q", expected, shape.Functions[0].Signature)
	}
}

func TestPyClassBasic(t *testing.T) {
	src := `class Foo:
    name: str
    count: int = 0

    def method(self, x: int) -> str:
        return str(x)

    def other(self):
        pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}

	td := shape.Types[0]
	if td.Name != "Foo" || td.Kind != "class" {
		t.Errorf("expected Foo/class, got %s/%s", td.Name, td.Kind)
	}

	if len(td.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d: %v", len(td.Fields), td.Fields)
	}
	if td.Fields[0].Name != "name" || td.Fields[0].Type != "str" {
		t.Errorf("field[0]: expected name:str, got %s:%s", td.Fields[0].Name, td.Fields[0].Type)
	}
	if td.Fields[1].Name != "count" || td.Fields[1].Type != "int" {
		t.Errorf("field[1]: expected count:int, got %s:%s", td.Fields[1].Name, td.Fields[1].Type)
	}

	if len(td.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d: %v", len(td.Methods), td.Methods)
	}
	if td.Methods[0].Name != "method" {
		t.Errorf("method[0]: expected 'method', got %q", td.Methods[0].Name)
	}
	if td.Methods[1].Name != "other" {
		t.Errorf("method[1]: expected 'other', got %q", td.Methods[1].Name)
	}
}

func TestPyClassWithBases(t *testing.T) {
	src := `class MyClass(Base1, Base2):
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}
	td := shape.Types[0]
	if len(td.Embeds) != 2 {
		t.Fatalf("expected 2 bases, got %d: %v", len(td.Embeds), td.Embeds)
	}
	if td.Embeds[0] != "Base1" || td.Embeds[1] != "Base2" {
		t.Errorf("expected [Base1, Base2], got %v", td.Embeds)
	}
}

func TestPyClassDecorators(t *testing.T) {
	src := `class Foo:
    @property
    def name(self) -> str:
        return self._name

    @staticmethod
    def create() -> None:
        pass

    @classmethod
    def from_dict(cls, data: dict) -> None:
        pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}
	td := shape.Types[0]
	if len(td.Methods) != 3 {
		t.Fatalf("expected 3 methods, got %d: %v", len(td.Methods), td.Methods)
	}

	if td.Methods[0].Signature != "@property (self) -> str" {
		t.Errorf("method[0] sig: expected '@property (self) -> str', got %q", td.Methods[0].Signature)
	}
	if td.Methods[1].Signature != "@staticmethod () -> None" {
		t.Errorf("method[1] sig: expected '@staticmethod () -> None', got %q", td.Methods[1].Signature)
	}
	if td.Methods[2].Signature != "@classmethod (cls, data: dict) -> None" {
		t.Errorf("method[2] sig: expected '@classmethod (cls, data: dict) -> None', got %q", td.Methods[2].Signature)
	}
}

func TestPyVisibilityExportedOnly(t *testing.T) {
	src := `def public_func():
    pass

def _private_func():
    pass

def __mangled_func():
    pass

def __dunder__():
    pass

class PublicClass:
    name: str
    _secret: int

    def public_method(self):
        pass

    def _private_method(self):
        pass

class _PrivateClass:
    pass

VERSION = "1.0"
_internal = "hidden"
`
	path := writeTempPy(t, src)

	// Exported only
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}

	// Functions: public_func and __dunder__
	if len(shape.Functions) != 2 {
		t.Fatalf("exported functions: expected 2, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if shape.Functions[0].Name != "public_func" {
		t.Errorf("expected public_func, got %q", shape.Functions[0].Name)
	}
	if shape.Functions[1].Name != "__dunder__" {
		t.Errorf("expected __dunder__, got %q", shape.Functions[1].Name)
	}

	// Types: only PublicClass
	if len(shape.Types) != 1 {
		t.Fatalf("exported types: expected 1, got %d: %v", len(shape.Types), shape.Types)
	}
	if shape.Types[0].Name != "PublicClass" {
		t.Errorf("expected PublicClass, got %q", shape.Types[0].Name)
	}

	// PublicClass fields: only "name" (not _secret)
	if len(shape.Types[0].Fields) != 1 || shape.Types[0].Fields[0].Name != "name" {
		t.Errorf("expected only 'name' field, got %v", shape.Types[0].Fields)
	}

	// PublicClass methods: only public_method
	if len(shape.Types[0].Methods) != 1 || shape.Types[0].Methods[0].Name != "public_method" {
		t.Errorf("expected only 'public_method', got %v", shape.Types[0].Methods)
	}

	// Variables: only VERSION
	if len(shape.Variables) != 1 || shape.Variables[0].Name != "VERSION" {
		t.Errorf("expected only VERSION variable, got %v", shape.Variables)
	}
}

func TestPyVisibilityAll(t *testing.T) {
	src := `def public_func():
    pass

def _private_func():
    pass

VERSION = "1.0"
_internal = "hidden"
`
	path := writeTempPy(t, src)

	shape, err := (&PyExtractor{}).Extract(path, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(shape.Functions) != 2 {
		t.Fatalf("all functions: expected 2, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if len(shape.Variables) != 2 {
		t.Fatalf("all variables: expected 2, got %d: %v", len(shape.Variables), shape.Variables)
	}
}

func TestPyNestedBlocks(t *testing.T) {
	src := `def outer():
    if True:
        for i in range(10):
            while i > 0:
                pass

def after():
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if shape.Functions[0].Name != "outer" || shape.Functions[1].Name != "after" {
		t.Errorf("expected outer/after, got %s/%s", shape.Functions[0].Name, shape.Functions[1].Name)
	}
}

func TestPyTripleQuotedStrings(t *testing.T) {
	src := `"""
Module docstring with def fake():
    class NotReal:
        pass
"""

def real_func():
    pass

x = '''
another triple-quoted
string with import os
'''

def second():
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if shape.Functions[0].Name != "real_func" || shape.Functions[1].Name != "second" {
		t.Errorf("expected real_func/second, got %s/%s",
			shape.Functions[0].Name, shape.Functions[1].Name)
	}
}

func TestPyCommentsInSignatures(t *testing.T) {
	src := `def func_with_comments(
    a: int,  # first arg
    b: str,  # second arg
) -> bool:  # return type
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(shape.Functions))
	}
	expected := "(a: int, b: str) -> bool"
	if shape.Functions[0].Signature != expected {
		t.Errorf("expected %q, got %q", expected, shape.Functions[0].Signature)
	}
}

func TestPyEmptyFile(t *testing.T) {
	path := writeTempPy(t, "")
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 0 || len(shape.Variables) != 0 || len(shape.Imports) != 0 || len(shape.Types) != 0 {
		t.Errorf("expected empty shape for empty file")
	}
}

func TestPyCommentOnlyFile(t *testing.T) {
	src := `# Just comments
# Nothing else here
# More comments
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 0 || len(shape.Types) != 0 {
		t.Errorf("expected empty shape for comment-only file, got funcs=%d types=%d",
			len(shape.Functions), len(shape.Types))
	}
}

func TestPyModuleVariables(t *testing.T) {
	src := `VERSION = "1.0.0"
MAX: int = 100
NAME = "hello"
DEBUG = True
COUNT = 42
NONE_VAL = None
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Variables) != 6 {
		t.Fatalf("expected 6 variables, got %d: %v", len(shape.Variables), shape.Variables)
	}

	tests := []struct {
		name, typ, value string
	}{
		{"VERSION", "", `"1.0.0"`},
		{"MAX", "int", "100"},
		{"NAME", "", `"hello"`},
		{"DEBUG", "", "True"},
		{"COUNT", "", "42"},
		{"NONE_VAL", "", "None"},
	}
	for i, tt := range tests {
		v := shape.Variables[i]
		if v.Name != tt.name {
			t.Errorf("var[%d]: expected name %q, got %q", i, tt.name, v.Name)
		}
		if v.Type != tt.typ {
			t.Errorf("var[%d] %q: expected type %q, got %q", i, tt.name, tt.typ, v.Type)
		}
		if v.Value != tt.value {
			t.Errorf("var[%d] %q: expected value %q, got %q", i, tt.name, tt.value, v.Value)
		}
	}
}

func TestPyLineNumbers(t *testing.T) {
	src := `# line 1
# line 2
def first():  # line 3
    pass

# line 6
def second():  # line 7
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
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

func TestPyClassLineNumber(t *testing.T) {
	src := `# header
# more header

class MyClass:  # line 4
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}
	if shape.Types[0].Line != 4 {
		t.Errorf("expected line 4, got %d", shape.Types[0].Line)
	}
}

func TestPyModuleLevelIfSkipped(t *testing.T) {
	src := `VERSION = "1.0"

if __name__ == "__main__":
    main()
    print("done")

AFTER = "visible"
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Variables) != 2 {
		t.Fatalf("expected 2 variables, got %d: %v", len(shape.Variables), shape.Variables)
	}
	if shape.Variables[0].Name != "VERSION" || shape.Variables[1].Name != "AFTER" {
		t.Errorf("expected VERSION/AFTER, got %s/%s",
			shape.Variables[0].Name, shape.Variables[1].Name)
	}
}

func TestPyTypeStubStyle(t *testing.T) {
	src := `from typing import Optional, List

def connect(host: str, port: int = 8080) -> bool: ...
def disconnect() -> None: ...

class Client:
    host: str
    port: int

    def send(self, data: bytes) -> int: ...
    def recv(self, size: int) -> Optional[bytes]: ...
`
	path := writeTempPyi(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(shape.Imports) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(shape.Imports), shape.Imports)
	}

	if len(shape.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d: %v", len(shape.Functions), shape.Functions)
	}
	if shape.Functions[0].Name != "connect" || shape.Functions[1].Name != "disconnect" {
		t.Errorf("expected connect/disconnect, got %v", shape.Functions)
	}

	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}
	td := shape.Types[0]
	if td.Name != "Client" {
		t.Errorf("expected Client, got %q", td.Name)
	}
	if len(td.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(td.Fields))
	}
	if len(td.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(td.Methods))
	}
}

func TestPyClassVariableAssignment(t *testing.T) {
	src := `class Config:
    DEFAULT_TIMEOUT: int = 30
    MAX_RETRIES = 3
    name: str
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}
	td := shape.Types[0]
	if len(td.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d: %v", len(td.Fields), td.Fields)
	}

	tests := []struct {
		name, typ string
	}{
		{"DEFAULT_TIMEOUT", "int"},
		{"MAX_RETRIES", ""},
		{"name", "str"},
	}
	for i, tt := range tests {
		if td.Fields[i].Name != tt.name {
			t.Errorf("field[%d]: expected name %q, got %q", i, tt.name, td.Fields[i].Name)
		}
		if td.Fields[i].Type != tt.typ {
			t.Errorf("field[%d] %q: expected type %q, got %q", i, tt.name, tt.typ, td.Fields[i].Type)
		}
	}
}

func TestPyMultipleClasses(t *testing.T) {
	src := `class First:
    def method_a(self):
        pass

class Second(First):
    def method_b(self):
        pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 2 {
		t.Fatalf("expected 2 types, got %d: %v", len(shape.Types), shape.Types)
	}
	if shape.Types[0].Name != "First" || shape.Types[1].Name != "Second" {
		t.Errorf("expected First/Second, got %s/%s", shape.Types[0].Name, shape.Types[1].Name)
	}
	if len(shape.Types[1].Embeds) != 1 || shape.Types[1].Embeds[0] != "First" {
		t.Errorf("expected Second to embed First, got %v", shape.Types[1].Embeds)
	}
}

func TestPyImportMultiple(t *testing.T) {
	src := `import os, sys, json
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"os", "sys", "json"}
	if len(shape.Imports) != len(expected) {
		t.Fatalf("expected %d imports, got %d: %v", len(expected), len(shape.Imports), shape.Imports)
	}
	for i, want := range expected {
		if shape.Imports[i] != want {
			t.Errorf("import[%d]: expected %q, got %q", i, want, shape.Imports[i])
		}
	}
}

func TestPyStringEscapes(t *testing.T) {
	src := `PATTERN = "has \"quotes\" inside"
SINGLE = 'single\'s quote'

def real():
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "real" {
		t.Errorf("expected 1 function 'real', got %v", shape.Functions)
	}
	if len(shape.Variables) != 2 {
		t.Fatalf("expected 2 variables, got %d", len(shape.Variables))
	}
}

func TestPyEqualityNotAssignment(t *testing.T) {
	src := `x == 5
a != b
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Variables) != 0 {
		t.Errorf("expected 0 variables for comparisons, got %d: %v", len(shape.Variables), shape.Variables)
	}
}

func TestPyClassWithTryExcept(t *testing.T) {
	src := `class Handler:
    def process(self):
        try:
            data = self.read()
        except Exception:
            pass
        finally:
            self.close()

    def next_method(self):
        pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}
	if len(shape.Types[0].Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d: %v", len(shape.Types[0].Methods), shape.Types[0].Methods)
	}
}

func TestPyFStringPrefix(t *testing.T) {
	src := `GREETING = f"Hello {name}"
RAW = r"\n not newline"

def real():
    pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Functions) != 1 || shape.Functions[0].Name != "real" {
		t.Errorf("expected 1 function 'real', got %v", shape.Functions)
	}
}

func TestPyMultipleDecorators(t *testing.T) {
	src := `class Foo:
    @property
    @abstractmethod
    def value(self) -> int:
        pass
`
	path := writeTempPy(t, src)
	shape, err := (&PyExtractor{}).Extract(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(shape.Types))
	}
	if len(shape.Types[0].Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(shape.Types[0].Methods))
	}
	m := shape.Types[0].Methods[0]
	if m.Name != "value" {
		t.Errorf("expected name 'value', got %q", m.Name)
	}
	expected := "@property @abstractmethod (self) -> int"
	if m.Signature != expected {
		t.Errorf("expected %q, got %q", expected, m.Signature)
	}
}
