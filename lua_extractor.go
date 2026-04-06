package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LuaExtractor extracts API surface from Lua source files.
type LuaExtractor struct{}

func init() {
	registerExtractor(&LuaExtractor{})
}

func (e *LuaExtractor) Extensions() []string {
	return []string{".lua"}
}

func (e *LuaExtractor) Extract(filePath string, exportedOnly bool) (*FileShape, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}

	s := &luaScanner{src: string(data), line: 1, exportedOnly: exportedOnly}
	shape := &FileShape{
		File:    filePath,
		Package: strings.TrimSuffix(filepath.Base(filePath), ".lua"),
	}
	s.parse(shape)
	return shape, nil
}

// Scanner

type luaScanner struct {
	src          string
	pos          int
	line         int
	depth        int // keyword block depth (function/if/do/repeat)
	braceDepth   int // table constructor depth ({})
	exportedOnly bool
}

func (s *luaScanner) eof() bool { return s.pos >= len(s.src) }

func (s *luaScanner) peek() byte {
	if s.eof() {
		return 0
	}
	return s.src[s.pos]
}

func (s *luaScanner) advance() {
	if s.pos < len(s.src) {
		if s.src[s.pos] == '\n' {
			s.line++
		}
		s.pos++
	}
}

// Whitespace and comments

func (s *luaScanner) skipWhitespace() {
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			return
		}
		if ch == '\n' {
			s.line++
		}
		s.pos++
	}
}

func (s *luaScanner) atComment() bool {
	return s.pos+1 < len(s.src) && s.src[s.pos] == '-' && s.src[s.pos+1] == '-'
}

// longBracketLevel returns the level of a long bracket at current position,
// or -1 if not a long bracket. Long brackets: [[ (level 0), [=[ (level 1), etc.
func (s *luaScanner) longBracketLevel() int {
	if s.pos >= len(s.src) || s.src[s.pos] != '[' {
		return -1
	}
	i := s.pos + 1
	level := 0
	for i < len(s.src) && s.src[i] == '=' {
		level++
		i++
	}
	if i < len(s.src) && s.src[i] == '[' {
		return level
	}
	return -1
}

// skipLongStringBody skips content until the matching close bracket ]===].
// The opening [=*[ must already be consumed.
func (s *luaScanner) skipLongStringBody(level int) {
	closer := "]" + strings.Repeat("=", level) + "]"
	for s.pos < len(s.src) {
		if s.src[s.pos] == '\n' {
			s.line++
		}
		if s.pos+len(closer) <= len(s.src) && s.src[s.pos:s.pos+len(closer)] == closer {
			s.pos += len(closer)
			return
		}
		s.pos++
	}
}

func (s *luaScanner) skipComment() {
	s.pos += 2 // skip --
	level := s.longBracketLevel()
	if level >= 0 {
		s.pos += 2 + level // skip [=*[
		s.skipLongStringBody(level)
		return
	}
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

func (s *luaScanner) skipWhitespaceAndComments() {
	for !s.eof() {
		s.skipWhitespace()
		if s.eof() || !s.atComment() {
			return
		}
		s.skipComment()
	}
}

// Strings

func (s *luaScanner) skipString() {
	ch := s.src[s.pos]
	if ch == '[' {
		level := s.longBracketLevel()
		if level >= 0 {
			s.pos += 2 + level
			s.skipLongStringBody(level)
			return
		}
	}
	quote := ch
	s.advance()
	for !s.eof() {
		c := s.src[s.pos]
		if c == '\\' {
			s.advance()
			if !s.eof() {
				s.advance()
			}
			continue
		}
		if c == quote {
			s.advance()
			return
		}
		if c == '\n' {
			return
		}
		s.advance()
	}
}

// readStringContent reads a quoted string and returns content without quotes.
func (s *luaScanner) readStringContent() string {
	quote := s.src[s.pos]
	s.advance()
	start := s.pos
	for !s.eof() {
		c := s.src[s.pos]
		if c == '\\' {
			s.advance()
			if !s.eof() {
				s.advance()
			}
			continue
		}
		if c == quote {
			content := s.src[start:s.pos]
			s.advance()
			return content
		}
		if c == '\n' {
			return s.src[start:s.pos]
		}
		s.advance()
	}
	return s.src[start:s.pos]
}

// Identifiers

func luaIsIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func luaIsIdentChar(ch byte) bool {
	return luaIsIdentStart(ch) || (ch >= '0' && ch <= '9')
}

func (s *luaScanner) readWord() string {
	start := s.pos
	for s.pos < len(s.src) && luaIsIdentChar(s.src[s.pos]) {
		s.pos++
	}
	return s.src[start:s.pos]
}

func (s *luaScanner) peekWord() string {
	saved := s.pos
	w := s.readWord()
	s.pos = saved
	return w
}

// Parameters

// readParamList reads (a, b, c, ...) and returns a normalized string.
func (s *luaScanner) readParamList() string {
	if s.eof() || s.src[s.pos] != '(' {
		return "()"
	}
	s.advance() // skip (

	var params []string
	for !s.eof() {
		s.skipWhitespaceAndComments()
		if s.eof() || s.peek() == ')' {
			break
		}
		if s.pos+3 <= len(s.src) && s.src[s.pos:s.pos+3] == "..." {
			params = append(params, "...")
			s.pos += 3
		} else if luaIsIdentStart(s.peek()) {
			params = append(params, s.readWord())
		} else {
			s.advance()
			continue
		}
		s.skipWhitespaceAndComments()
		if !s.eof() && s.peek() == ',' {
			s.advance()
		}
	}
	if !s.eof() && s.peek() == ')' {
		s.advance()
	}
	return "(" + strings.Join(params, ", ") + ")"
}

// skipFunctionSignature skips optional name and params of a function at depth > 0.
func (s *luaScanner) skipFunctionSignature() {
	s.skipWhitespaceAndComments()
	if !s.eof() && luaIsIdentStart(s.peek()) {
		s.readWord()
		for !s.eof() && (s.peek() == '.' || s.peek() == ':') {
			s.advance()
			if !s.eof() && luaIsIdentStart(s.peek()) {
				s.readWord()
			}
		}
	}
	s.skipWhitespaceAndComments()
	if !s.eof() && s.peek() == '(' {
		s.readParamList()
	}
}

// Require

func (s *luaScanner) readRequireArg() string {
	if s.eof() {
		return ""
	}
	ch := s.peek()
	if ch == '(' {
		s.advance()
		s.skipWhitespaceAndComments()
		if s.eof() {
			return ""
		}
		ch = s.peek()
		if ch == '"' || ch == '\'' {
			mod := s.readStringContent()
			s.skipWhitespaceAndComments()
			if !s.eof() && s.peek() == ')' {
				s.advance()
			}
			return mod
		}
		return ""
	}
	if ch == '"' || ch == '\'' {
		return s.readStringContent()
	}
	return ""
}

// Simple value peeking

// peekSimpleValue returns a simple literal value without advancing the scanner.
func (s *luaScanner) peekSimpleValue() string {
	saved := s.pos
	savedLine := s.line

	s.skipWhitespaceAndComments()
	if s.eof() {
		s.pos = saved
		s.line = savedLine
		return ""
	}

	ch := s.peek()
	var result string

	switch {
	case ch == '"' || ch == '\'':
		content := s.readStringContent()
		result = string(ch) + content + string(ch)

	case ch >= '0' && ch <= '9',
		ch == '.' && s.pos+1 < len(s.src) && s.src[s.pos+1] >= '0' && s.src[s.pos+1] <= '9':
		start := s.pos
		for !s.eof() {
			c := s.peek()
			isDigit := c >= '0' && c <= '9'
			isHex := (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			isMeta := c == '.' || c == 'x' || c == 'X' || c == 'e' || c == 'E' || c == '+' || c == '-'
			if !isDigit && !isHex && !isMeta {
				break
			}
			s.advance()
		}
		result = s.src[start:s.pos]

	default:
		w := s.peekWord()
		if w == "true" || w == "false" || w == "nil" {
			result = w
		}
	}

	s.pos = saved
	s.line = savedLine
	return result
}

// Main parse loop

func (s *luaScanner) parse(shape *FileShape) {
	for !s.eof() {
		s.skipWhitespaceAndComments()
		if s.eof() {
			break
		}

		ch := s.peek()

		// Strings
		if ch == '"' || ch == '\'' {
			s.skipString()
			continue
		}
		if ch == '[' && s.longBracketLevel() >= 0 {
			s.skipString()
			continue
		}

		// Table constructors
		if ch == '{' {
			s.advance()
			s.braceDepth++
			continue
		}
		if ch == '}' {
			s.advance()
			if s.braceDepth > 0 {
				s.braceDepth--
			}
			continue
		}

		// Non-identifier: advance
		if !luaIsIdentStart(ch) {
			s.advance()
			continue
		}

		word := s.peekWord()
		atTopLevel := s.depth == 0 && s.braceDepth == 0

		switch word {
		case "local":
			s.readWord()
			s.skipWhitespaceAndComments()
			if atTopLevel {
				s.parseLocal(shape)
			} else {
				// Still need depth tracking for local function at depth > 0
				if s.peekWord() == "function" {
					s.readWord()
					s.skipFunctionSignature()
					s.depth++
				}
			}

		case "function":
			line := s.line
			s.readWord()
			s.skipWhitespaceAndComments()
			if atTopLevel {
				s.parseFunctionDecl(shape, line)
			} else {
				s.skipFunctionSignature()
			}
			s.depth++

		case "end":
			s.readWord()
			if s.depth > 0 {
				s.depth--
			}

		case "until":
			s.readWord()
			if s.depth > 0 {
				s.depth--
			}

		case "if", "do", "repeat":
			s.readWord()
			s.depth++

		case "for", "while", "return", "then", "else", "elseif", "in",
			"break", "goto", "and", "or", "not", "true", "false", "nil":
			s.readWord()

		default:
			if atTopLevel {
				s.parseTopLevelIdent(shape)
			} else {
				s.readWord()
			}
		}
	}
}

// Local declarations

func (s *luaScanner) parseLocal(shape *FileShape) {
	if s.eof() {
		return
	}

	// local function name(params)
	if s.peekWord() == "function" {
		line := s.line
		s.readWord()
		s.skipWhitespaceAndComments()
		name := s.readWord()
		s.skipWhitespaceAndComments()
		params := s.readParamList()
		s.depth++

		if !s.exportedOnly && name != "" {
			shape.Functions = append(shape.Functions, FuncDef{
				Name:      name,
				Signature: params,
				Line:      line,
			})
		}
		return
	}

	// local name [, name2] [= expr, ...]
	line := s.line
	name := s.readWord()
	if name == "" {
		return
	}

	names := []string{name}
	for {
		s.skipWhitespaceAndComments()
		if s.eof() || s.peek() != ',' {
			break
		}
		s.advance()
		s.skipWhitespaceAndComments()
		n := s.readWord()
		if n == "" {
			break
		}
		names = append(names, n)
	}

	s.skipWhitespaceAndComments()

	// No assignment
	if s.eof() || s.peek() != '=' {
		if !s.exportedOnly {
			for _, n := range names {
				shape.Variables = append(shape.Variables, ValueDef{Name: n, Line: line})
			}
		}
		return
	}
	// Distinguish = from ==
	if s.pos+1 < len(s.src) && s.src[s.pos+1] == '=' {
		return
	}

	s.advance() // skip =
	s.skipWhitespaceAndComments()

	// require
	if s.peekWord() == "require" {
		s.readWord()
		s.skipWhitespaceAndComments()
		mod := s.readRequireArg()
		if mod != "" {
			shape.Imports = append(shape.Imports, names[0]+" "+mod)
			return
		}
	}

	// function expression
	if s.peekWord() == "function" {
		fline := s.line
		s.readWord()
		s.skipWhitespaceAndComments()
		params := s.readParamList()
		s.depth++
		if !s.exportedOnly {
			shape.Functions = append(shape.Functions, FuncDef{
				Name:      names[0],
				Signature: params,
				Line:      fline,
			})
		}
		return
	}

	// Regular local variable
	if !s.exportedOnly {
		for _, n := range names {
			shape.Variables = append(shape.Variables, ValueDef{Name: n, Line: line})
		}
	}
}

// Function declarations

func (s *luaScanner) parseFunctionDecl(shape *FileShape, line int) {
	// Anonymous function
	if s.eof() || s.peek() == '(' {
		s.readParamList()
		return
	}

	name := s.readWord()
	if name == "" {
		s.readParamList()
		return
	}

	receiver := ""
	isColon := false

	for !s.eof() && (s.peek() == '.' || s.peek() == ':') {
		sep := s.peek()
		s.advance()
		s.skipWhitespaceAndComments()
		next := s.readWord()
		if sep == ':' {
			if receiver != "" {
				receiver = receiver + "." + name
			} else {
				receiver = name
			}
			name = next
			isColon = true
			break
		}
		if receiver != "" {
			receiver = receiver + "." + name
		} else {
			receiver = name
		}
		name = next
	}

	s.skipWhitespaceAndComments()
	params := s.readParamList()

	if isColon {
		inner := ""
		if len(params) > 2 {
			inner = params[1 : len(params)-1] // strip ( and )
		}
		if inner == "" {
			params = "(self)"
		} else {
			params = "(self, " + inner + ")"
		}
	}

	shape.Functions = append(shape.Functions, FuncDef{
		Name:      name,
		Receiver:  receiver,
		Signature: params,
		Line:      line,
	})
}

// Top-level identifiers and assignments

func (s *luaScanner) parseTopLevelIdent(shape *FileShape) {
	// Bare require call
	if s.peekWord() == "require" {
		s.readWord()
		s.skipWhitespaceAndComments()
		mod := s.readRequireArg()
		if mod != "" {
			shape.Imports = append(shape.Imports, mod)
		}
		return
	}

	line := s.line
	name := s.readWord()

	// Build dotted name: M.field, M.sub.field
	fullName := name
	for !s.eof() && s.peek() == '.' {
		s.advance()
		next := s.readWord()
		if next == "" {
			break
		}
		fullName = fullName + "." + next
	}

	s.skipWhitespaceAndComments()

	// Not an assignment
	if s.eof() || s.peek() != '=' {
		return
	}
	// Distinguish = from ==
	if s.pos+1 < len(s.src) && s.src[s.pos+1] == '=' {
		return
	}

	s.advance() // skip =
	s.skipWhitespaceAndComments()

	// require
	if s.peekWord() == "require" {
		s.readWord()
		s.skipWhitespaceAndComments()
		mod := s.readRequireArg()
		if mod != "" {
			shape.Imports = append(shape.Imports, fullName+" "+mod)
			return
		}
	}

	// function expression: M.foo = function(a, b)
	if s.peekWord() == "function" {
		fline := s.line
		s.readWord()
		s.skipWhitespaceAndComments()
		params := s.readParamList()
		s.depth++

		fd := FuncDef{
			Name:      fullName,
			Signature: params,
			Line:      fline,
		}
		if idx := strings.LastIndex(fullName, "."); idx >= 0 {
			fd.Receiver = fullName[:idx]
			fd.Name = fullName[idx+1:]
		}
		shape.Functions = append(shape.Functions, fd)
		return
	}

	// Regular assignment
	vd := ValueDef{
		Name:  fullName,
		Value: s.peekSimpleValue(),
		Line:  line,
	}
	shape.Variables = append(shape.Variables, vd)
}
