package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PyExtractor extracts API surface from Python source files.
type PyExtractor struct{}

func init() {
	registerExtractor(&PyExtractor{})
}

func (e *PyExtractor) Extensions() []string {
	return []string{".py", ".pyi"}
}

func (e *PyExtractor) Extract(filePath string, exportedOnly bool) (*FileShape, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}

	ext := filepath.Ext(filePath)
	base := filepath.Base(filePath)
	pkg := strings.TrimSuffix(base, ext)

	s := &pyScanner{src: string(data), line: 1, exportedOnly: exportedOnly}
	shape := &FileShape{
		File:    filePath,
		Package: pkg,
	}
	s.parse(shape)
	return shape, nil
}

// Scanner

type pyScanner struct {
	src          string
	pos          int
	line         int
	classIndent  int // indent of current class line, -1 when not in class
	skipIndent   int // indent of block being skipped, -1 when not skipping
	exportedOnly bool
	currentClass *TypeDef // pointer to the class being built
}

func (s *pyScanner) eof() bool { return s.pos >= len(s.src) }

func (s *pyScanner) peek() byte {
	if s.eof() {
		return 0
	}
	return s.src[s.pos]
}

func (s *pyScanner) advance() {
	if s.pos < len(s.src) {
		if s.src[s.pos] == '\n' {
			s.line++
		}
		s.pos++
	}
}

// Line reading

// measureIndent counts the number of spaces at the beginning of the current line.
// Tabs count as 4 spaces each (matching Python convention).
func (s *pyScanner) measureIndent() int {
	indent := 0
	i := s.pos
	for i < len(s.src) {
		if s.src[i] == ' ' {
			indent++
			i++
		} else if s.src[i] == '\t' {
			indent += 4
			i++
		} else {
			break
		}
	}
	return indent
}

// skipToEndOfLine advances past everything until (and including) the newline.
func (s *pyScanner) skipToEndOfLine() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
	if s.pos < len(s.src) {
		s.line++
		s.pos++
	}
}

// skipToNextLine is the same as skipToEndOfLine (alias for clarity).
func (s *pyScanner) skipToNextLine() {
	s.skipToEndOfLine()
}

// skipIndentWhitespace advances past leading whitespace (spaces, tabs) without crossing newlines.
func (s *pyScanner) skipIndentWhitespace() {
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch == ' ' || ch == '\t' {
			s.pos++
		} else {
			return
		}
	}
}

// isBlankLine checks if the current position is at a blank line (only whitespace before newline/EOF).
func (s *pyScanner) isBlankLine() bool {
	i := s.pos
	for i < len(s.src) {
		ch := s.src[i]
		if ch == '\n' || ch == '\r' {
			return true
		}
		if ch != ' ' && ch != '\t' {
			return false
		}
		i++
	}
	return true // EOF counts as blank
}

// isCommentLine checks if the current line (after indent whitespace) starts with #.
func (s *pyScanner) isCommentLine() bool {
	i := s.pos
	for i < len(s.src) {
		ch := s.src[i]
		if ch == '#' {
			return true
		}
		if ch != ' ' && ch != '\t' {
			return false
		}
		i++
	}
	return false
}

// Identifiers

func pyIsIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func pyIsIdentChar(ch byte) bool {
	return pyIsIdentStart(ch) || (ch >= '0' && ch <= '9')
}

func (s *pyScanner) readWord() string {
	start := s.pos
	for s.pos < len(s.src) && pyIsIdentChar(s.src[s.pos]) {
		s.pos++
	}
	return s.src[start:s.pos]
}

func (s *pyScanner) peekWord() string {
	saved := s.pos
	w := s.readWord()
	s.pos = saved
	return w
}

// Strings

// skipTripleQuotedString skips a triple-quoted string (""" or ”').
// The opening triple-quote has already been consumed.
func (s *pyScanner) skipTripleQuotedString(quote byte) {
	for s.pos < len(s.src) {
		if s.src[s.pos] == '\\' {
			s.advance()
			if !s.eof() {
				s.advance()
			}
			continue
		}
		if s.src[s.pos] == quote && s.pos+2 < len(s.src) && s.src[s.pos+1] == quote && s.src[s.pos+2] == quote {
			s.advance()
			s.advance()
			s.advance()
			return
		}
		if s.src[s.pos] == '\n' {
			s.line++
		}
		s.pos++
	}
}

// isTripleQuote checks if current pos starts a triple-quote.
func (s *pyScanner) isTripleQuote() (byte, bool) {
	if s.pos+2 >= len(s.src) {
		return 0, false
	}
	ch := s.src[s.pos]
	if (ch == '"' || ch == '\'') && s.src[s.pos+1] == ch && s.src[s.pos+2] == ch {
		return ch, true
	}
	return 0, false
}

// skipSingleQuotedString skips a single-quoted string. The opening quote has NOT been consumed.
func (s *pyScanner) skipSingleQuotedString() {
	quote := s.src[s.pos]
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

// skipStringAtPos skips any string literal at current position (checking for triple quotes first).
func (s *pyScanner) skipStringAtPos() {
	if q, ok := s.isTripleQuote(); ok {
		s.advance()
		s.advance()
		s.advance()
		s.skipTripleQuotedString(q)
	} else {
		s.skipSingleQuotedString()
	}
}

// skipStringPrefix skips f/r/b/u/rb/br string prefixes and returns true if a quote follows.
func (s *pyScanner) skipStringPrefix() bool {
	start := s.pos
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch == 'f' || ch == 'r' || ch == 'b' || ch == 'u' || ch == 'F' || ch == 'R' || ch == 'B' || ch == 'U' {
			s.pos++
		} else {
			break
		}
	}
	if s.pos > start && s.pos < len(s.src) && (s.src[s.pos] == '"' || s.src[s.pos] == '\'') {
		return true
	}
	s.pos = start
	return false
}

// Comments

func (s *pyScanner) skipComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

// Visibility

func pyIsPrivate(name string) bool {
	if len(name) == 0 {
		return false
	}
	// Dunder names (__name__) are public
	if len(name) >= 4 && name[0] == '_' && name[1] == '_' && name[len(name)-1] == '_' && name[len(name)-2] == '_' {
		return false
	}
	// Single underscore prefix or double underscore prefix (name mangling) = private
	if name[0] == '_' {
		return true
	}
	return false
}

// Import parsing

// readDottedName reads a dotted name like "foo.bar.baz".
// For relative imports, leading dots are preserved.
func (s *pyScanner) readDottedName() string {
	var buf strings.Builder
	// Handle leading dots for relative imports
	for s.pos < len(s.src) && s.src[s.pos] == '.' {
		buf.WriteByte('.')
		s.pos++
	}
	s.skipInlineWhitespace()
	// Check if next word is a keyword like "import" - don't consume it
	if s.pos < len(s.src) && pyIsIdentStart(s.src[s.pos]) {
		w := s.peekWord()
		if w == "import" {
			return buf.String()
		}
		s.readWord()
		buf.WriteString(w)
		for s.pos < len(s.src) && s.src[s.pos] == '.' {
			s.pos++
			if s.pos < len(s.src) && pyIsIdentStart(s.src[s.pos]) {
				next := s.peekWord()
				if next == "import" {
					// Put the dot back - it was a separator before "import"
					// Actually we consumed the dot, but "import" is the keyword
					// This shouldn't happen in valid Python, but be safe
					s.pos-- // unconsume the dot
					break
				}
				s.readWord()
				buf.WriteByte('.')
				buf.WriteString(next)
			}
		}
	}
	return buf.String()
}

// skipInlineWhitespace skips spaces and tabs but not newlines.
func (s *pyScanner) skipInlineWhitespace() {
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch == ' ' || ch == '\t' {
			s.pos++
		} else {
			return
		}
	}
}

// skipInlineWhitespaceAndComments skips spaces, tabs, and # comments within a line.
// Also handles backslash line continuation.
func (s *pyScanner) skipInlineWhitespaceAndContinuation() {
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch == ' ' || ch == '\t' {
			s.pos++
		} else if ch == '\\' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '\n' {
			s.pos++
			s.line++
			s.pos++
		} else if ch == '#' {
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
		} else {
			return
		}
	}
}

// parseImportStatement parses "import foo, foo.bar as baz".
func (s *pyScanner) parseImportStatement(shape *FileShape) {
	s.skipInlineWhitespace()
	for {
		s.skipInlineWhitespace()
		if s.eof() || s.src[s.pos] == '\n' || s.src[s.pos] == '#' {
			break
		}
		module := s.readDottedName()
		if module == "" {
			break
		}
		s.skipInlineWhitespace()
		if s.peekWord() == "as" {
			s.readWord()
			s.skipInlineWhitespace()
			alias := s.readWord()
			if alias != "" {
				shape.Imports = append(shape.Imports, alias+" "+module)
			} else {
				shape.Imports = append(shape.Imports, module)
			}
		} else {
			shape.Imports = append(shape.Imports, module)
		}
		s.skipInlineWhitespace()
		if s.pos < len(s.src) && s.src[s.pos] == ',' {
			s.pos++
		} else {
			break
		}
	}
}

// parseFromImportStatement parses "from foo import bar, baz as qux".
func (s *pyScanner) parseFromImportStatement(shape *FileShape) {
	s.skipInlineWhitespace()
	module := s.readDottedName()
	if module == "" {
		s.skipToEndOfLine()
		return
	}
	s.skipInlineWhitespace()
	if s.peekWord() != "import" {
		s.skipToEndOfLine()
		return
	}
	s.readWord() // consume "import"
	s.skipInlineWhitespace()

	// Check for parenthesized imports
	paren := false
	if s.pos < len(s.src) && s.src[s.pos] == '(' {
		paren = true
		s.pos++
	}

	// Check for star import
	if s.pos < len(s.src) && s.src[s.pos] == '*' {
		s.pos++
		shape.Imports = append(shape.Imports, module+".*")
		s.skipToEndOfLine()
		return
	}

	for {
		if paren {
			s.skipParenWhitespace()
		} else {
			s.skipInlineWhitespace()
		}
		if s.eof() {
			break
		}
		if paren && s.src[s.pos] == ')' {
			s.pos++
			break
		}
		if !paren && (s.src[s.pos] == '\n' || s.src[s.pos] == '#') {
			break
		}

		name := s.readWord()
		if name == "" {
			if paren {
				// Skip any stray characters
				s.advance()
				continue
			}
			break
		}

		if paren {
			s.skipParenWhitespace()
		} else {
			s.skipInlineWhitespace()
		}

		if s.peekWord() == "as" {
			s.readWord()
			if paren {
				s.skipParenWhitespace()
			} else {
				s.skipInlineWhitespace()
			}
			alias := s.readWord()
			if alias != "" {
				shape.Imports = append(shape.Imports, alias+" "+module+"."+name)
			} else {
				shape.Imports = append(shape.Imports, module+"."+name)
			}
		} else {
			shape.Imports = append(shape.Imports, module+"."+name)
		}

		if paren {
			s.skipParenWhitespace()
		} else {
			s.skipInlineWhitespace()
		}
		if s.pos < len(s.src) && s.src[s.pos] == ',' {
			s.pos++
		} else if !paren {
			break
		}
	}
}

// skipParenWhitespace skips whitespace including newlines inside parenthesized imports.
func (s *pyScanner) skipParenWhitespace() {
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			s.pos++
		} else if ch == '\n' {
			s.line++
			s.pos++
		} else if ch == '#' {
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
		} else if ch == '\\' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '\n' {
			s.pos++
			s.line++
			s.pos++
		} else {
			return
		}
	}
}

// Signature reading

// readSignature reads a function signature from the current position (starting at '(')
// and returns the normalized signature string. Handles multi-line signatures, comments,
// and type annotations.
func (s *pyScanner) readSignature() string {
	if s.eof() || s.src[s.pos] != '(' {
		return "()"
	}
	s.pos++ // skip (

	depth := 1
	var parts []byte
	parts = append(parts, '(')
	lastWasSpace := false

	for !s.eof() && depth > 0 {
		ch := s.src[s.pos]

		switch {
		case ch == '#':
			// Skip comment to end of line
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
			continue

		case ch == '\n':
			s.line++
			s.pos++
			// Treat newline as space
			if !lastWasSpace && len(parts) > 0 && parts[len(parts)-1] != '(' {
				parts = append(parts, ' ')
				lastWasSpace = true
			}
			continue

		case ch == '\r':
			s.pos++
			continue

		case ch == '\\' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '\n':
			s.pos++
			s.line++
			s.pos++
			continue

		case ch == '(':
			depth++
			parts = append(parts, ch)
			lastWasSpace = false
			s.pos++

		case ch == ')':
			depth--
			if depth == 0 {
				// Trim trailing space and comma before )
				for len(parts) > 1 && (parts[len(parts)-1] == ' ' || parts[len(parts)-1] == ',') {
					parts = parts[:len(parts)-1]
				}
				parts = append(parts, ')')
				s.pos++
			} else {
				parts = append(parts, ch)
				lastWasSpace = false
				s.pos++
			}

		case ch == '[':
			// Read bracket expression (type annotations like List[int])
			parts = append(parts, ch)
			lastWasSpace = false
			s.pos++

		case ch == ']':
			parts = append(parts, ch)
			lastWasSpace = false
			s.pos++

		case ch == '"' || ch == '\'':
			// String in default value
			start := s.pos
			s.skipStringAtPos()
			parts = append(parts, s.src[start:s.pos]...)
			lastWasSpace = false

		case ch == ' ' || ch == '\t':
			if !lastWasSpace && len(parts) > 0 && parts[len(parts)-1] != '(' {
				parts = append(parts, ' ')
				lastWasSpace = true
			}
			s.pos++

		default:
			parts = append(parts, ch)
			lastWasSpace = false
			s.pos++
		}
	}

	// Read return type annotation
	s.skipInlineWhitespace()
	if s.pos+1 < len(s.src) && s.src[s.pos] == '-' && s.src[s.pos+1] == '>' {
		s.pos += 2
		s.skipInlineWhitespace()
		retStart := s.pos
		// Read until colon or newline
		bracketDepth := 0
		for s.pos < len(s.src) {
			rc := s.src[s.pos]
			if rc == '[' {
				bracketDepth++
				s.pos++
			} else if rc == ']' {
				bracketDepth--
				s.pos++
			} else if rc == ':' && bracketDepth == 0 {
				break
			} else if rc == '\n' {
				break
			} else if rc == '#' {
				break
			} else {
				s.pos++
			}
		}
		retType := strings.TrimSpace(s.src[retStart:s.pos])
		if retType != "" {
			return string(parts) + " -> " + retType
		}
	}

	return string(parts)
}

// Simple value reading

// peekSimpleValue reads a simple literal value without advancing the scanner permanently.
func (s *pyScanner) peekSimpleValue() string {
	saved := s.pos
	savedLine := s.line

	s.skipInlineWhitespace()
	if s.eof() || s.src[s.pos] == '\n' {
		s.pos = saved
		s.line = savedLine
		return ""
	}

	ch := s.peek()
	var result string

	switch {
	case ch == '"' || ch == '\'':
		if q, ok := s.isTripleQuote(); ok {
			// Triple-quoted string: read it but don't extract value (too long)
			_ = q
		} else {
			start := s.pos
			s.skipSingleQuotedString()
			result = s.src[start:s.pos]
		}

	case ch >= '0' && ch <= '9':
		start := s.pos
		for !s.eof() {
			c := s.peek()
			isDigit := c >= '0' && c <= '9'
			isHex := (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			isMeta := c == '.' || c == 'x' || c == 'X' || c == 'e' || c == 'E' || c == '+' || c == '-' || c == '_' || c == 'o' || c == 'O' || c == 'b' || c == 'B' || c == 'j' || c == 'J'
			if !isDigit && !isHex && !isMeta {
				break
			}
			s.advance()
		}
		result = s.src[start:s.pos]

	case ch == '-' && s.pos+1 < len(s.src) && s.src[s.pos+1] >= '0' && s.src[s.pos+1] <= '9':
		start := s.pos
		s.advance() // skip -
		for !s.eof() {
			c := s.peek()
			isDigit := c >= '0' && c <= '9'
			isMeta := c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' || c == '_' || c == 'j' || c == 'J'
			if !isDigit && !isMeta {
				break
			}
			s.advance()
		}
		result = s.src[start:s.pos]

	default:
		if pyIsIdentStart(ch) {
			w := s.peekWord()
			if w == "True" || w == "False" || w == "None" {
				result = w
			} else if ch == 'f' || ch == 'r' || ch == 'b' || ch == 'u' || ch == 'F' || ch == 'R' || ch == 'B' || ch == 'U' {
				// Check for string prefix
				prefixStart := s.pos
				if s.skipStringPrefix() {
					start := prefixStart
					s.skipStringAtPos()
					result = s.src[start:s.pos]
				}
			}
		}
	}

	s.pos = saved
	s.line = savedLine
	return result
}

// Decorator reading

// readFirstDecorator reads a single decorator when the scanner is already positioned at '@'.
func (s *pyScanner) readFirstDecorator() []string {
	s.pos++ // skip @
	name := s.readWord()
	for s.pos < len(s.src) && s.src[s.pos] == '.' {
		s.pos++
		name += "." + s.readWord()
	}
	// Skip decorator arguments if present
	if s.pos < len(s.src) && s.src[s.pos] == '(' {
		depth := 1
		s.pos++
		for s.pos < len(s.src) && depth > 0 {
			c := s.src[s.pos]
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
			} else if c == '\n' {
				s.line++
			} else if c == '"' || c == '\'' {
				s.skipStringAtPos()
				continue
			}
			s.pos++
		}
	}
	result := []string{"@" + name}
	s.skipToNextLine()
	return result
}

// readDecorators reads decorator lines starting with @ and returns them as a list.
func (s *pyScanner) readDecorators(indent int) []string {
	var decorators []string
	for {
		savedPos := s.pos
		savedLine := s.line

		// Check if current line is blank or comment - skip
		if s.isBlankLine() {
			s.skipToNextLine()
			continue
		}
		if s.isCommentLine() {
			s.skipToNextLine()
			continue
		}

		lineIndent := s.measureIndent()
		if lineIndent != indent {
			s.pos = savedPos
			s.line = savedLine
			break
		}
		s.skipIndentWhitespace()

		if s.eof() || s.src[s.pos] != '@' {
			s.pos = savedPos
			s.line = savedLine
			break
		}

		s.pos++ // skip @
		// Read decorator name (possibly dotted)
		name := s.readWord()
		for s.pos < len(s.src) && s.src[s.pos] == '.' {
			s.pos++
			name += "." + s.readWord()
		}
		// Skip decorator arguments if present
		if s.pos < len(s.src) && s.src[s.pos] == '(' {
			depth := 1
			s.pos++
			for s.pos < len(s.src) && depth > 0 {
				c := s.src[s.pos]
				if c == '(' {
					depth++
				} else if c == ')' {
					depth--
				} else if c == '\n' {
					s.line++
				} else if c == '"' || c == '\'' {
					s.skipStringAtPos()
					continue
				}
				s.pos++
			}
		}
		decorators = append(decorators, "@"+name)
		s.skipToNextLine()
	}
	return decorators
}

// Main parse loop

func (s *pyScanner) parse(shape *FileShape) {
	s.classIndent = -1
	s.skipIndent = -1

	for !s.eof() {
		// Skip blank lines (no scope transitions)
		if s.isBlankLine() {
			s.skipToNextLine()
			continue
		}

		// Skip comment-only lines (no scope transitions)
		if s.isCommentLine() {
			s.skipToNextLine()
			continue
		}

		indent := s.measureIndent()
		s.skipIndentWhitespace()

		if s.eof() {
			break
		}

		ch := s.src[s.pos]

		// Handle triple-quoted strings at any position (docstrings, etc.)
		if ch == '"' || ch == '\'' {
			if q, ok := s.isTripleQuote(); ok {
				s.advance()
				s.advance()
				s.advance()
				s.skipTripleQuotedString(q)
				s.skipToNextLine()
				continue
			}
		}

		// Scope transitions based on indentation

		// If we are skipping a block and this line is deeper, skip it
		if s.skipIndent >= 0 && indent > s.skipIndent {
			s.skipToNextLine()
			continue
		}
		// If we were skipping and line is at or less than skipIndent, stop skipping
		if s.skipIndent >= 0 && indent <= s.skipIndent {
			s.skipIndent = -1
		}

		// If we are in a class and this line is at or less than classIndent, leave class
		if s.classIndent >= 0 && indent <= s.classIndent {
			s.finishClass(shape)
		}

		// Determine what level we're at
		atModuleLevel := indent == 0 && s.classIndent < 0
		atClassLevel := s.classIndent >= 0 && indent > s.classIndent && s.skipIndent < 0

		// Handle string prefixes (f"...", r"...", etc.)
		if pyIsIdentStart(ch) {
			savedPos := s.pos
			if s.skipStringPrefix() {
				s.skipStringAtPos()
				s.skipToNextLine()
				continue
			}
			s.pos = savedPos
		}

		// Parse based on first keyword
		word := s.peekWord()

		switch word {
		case "import":
			if atModuleLevel {
				s.readWord()
				s.parseImportStatement(shape)
			}
			s.skipToNextLine()

		case "from":
			if atModuleLevel {
				s.readWord()
				s.parseFromImportStatement(shape)
			}
			s.skipToNextLine()

		case "class":
			if atModuleLevel {
				s.parseClassDef(shape, indent)
			} else if atClassLevel {
				// Nested class inside a class body: skip its block
				s.skipIndent = indent
				s.skipToNextLine()
			} else {
				s.skipToNextLine()
			}

		case "def":
			if atModuleLevel {
				s.parseFuncDef(shape, nil, indent)
			} else if atClassLevel {
				s.parseFuncDef(shape, s.currentClass, indent)
			} else {
				s.skipToNextLine()
			}

		case "async":
			s.readWord()
			s.skipInlineWhitespace()
			if s.peekWord() == "def" {
				if atModuleLevel {
					s.parseAsyncFuncDef(shape, nil, indent)
				} else if atClassLevel {
					s.parseAsyncFuncDef(shape, s.currentClass, indent)
				} else {
					s.skipToNextLine()
				}
			} else {
				s.skipToNextLine()
			}

		case "if", "for", "while", "with", "try", "except", "finally", "else", "elif":
			if atModuleLevel || atClassLevel {
				s.skipIndent = indent
			}
			s.skipToNextLine()

		case "":
			if ch == '@' {
				// Read the first decorator (we're already positioned at @)
				decorators := s.readFirstDecorator()
				// Read any subsequent decorators from following lines
				more := s.readDecorators(indent)
				decorators = append(decorators, more...)
				// After decorators, we should be at the def/class line
				if !s.eof() {
					nextIndent := s.measureIndent()
					s.skipIndentWhitespace()
					nextWord := s.peekWord()
					switch nextWord {
					case "def":
						if atModuleLevel {
							s.parseDecoratedFuncDef(shape, nil, nextIndent, decorators)
						} else if atClassLevel {
							s.parseDecoratedFuncDef(shape, s.currentClass, nextIndent, decorators)
						} else {
							s.skipToNextLine()
						}
					case "async":
						s.readWord()
						s.skipInlineWhitespace()
						if s.peekWord() == "def" {
							if atModuleLevel {
								s.parseDecoratedAsyncFuncDef(shape, nil, nextIndent, decorators)
							} else if atClassLevel {
								s.parseDecoratedAsyncFuncDef(shape, s.currentClass, nextIndent, decorators)
							} else {
								s.skipToNextLine()
							}
						} else {
							s.skipToNextLine()
						}
					case "class":
						if indent == 0 && s.classIndent < 0 {
							s.parseDecoratedClassDef(shape, nextIndent, decorators)
						} else {
							s.skipToNextLine()
						}
					default:
						s.skipToNextLine()
					}
				}
			} else {
				// Non-identifier character at start of line
				s.skipToNextLine()
			}

		default:
			if atModuleLevel {
				s.parseModuleVariable(shape, indent)
			} else if atClassLevel {
				s.parseClassVariable(indent)
			} else {
				s.skipToNextLine()
			}
		}
	}

	// Finish any open class
	if s.classIndent >= 0 {
		s.finishClass(shape)
	}
}

// Class parsing

func (s *pyScanner) parseClassDef(shape *FileShape, indent int) {
	line := s.line
	s.readWord() // consume "class"
	s.skipInlineWhitespace()
	name := s.readWord()
	if name == "" {
		s.skipToNextLine()
		return
	}

	td := TypeDef{
		Name: name,
		Kind: "class",
		Line: line,
	}

	// Parse bases
	s.skipInlineWhitespace()
	if s.pos < len(s.src) && s.src[s.pos] == '(' {
		td.Embeds = s.readBaseClasses()
	}

	if s.exportedOnly && pyIsPrivate(name) {
		s.skipIndent = indent
		s.skipToNextLine()
		return
	}

	s.classIndent = indent
	s.currentClass = &td
	s.skipToNextLine()
}

func (s *pyScanner) parseDecoratedClassDef(shape *FileShape, indent int, decorators []string) {
	// Decorators on classes are stored but not displayed in current schema
	// Just parse the class normally
	s.parseClassDef(shape, indent)
}

func (s *pyScanner) readBaseClasses() []string {
	s.pos++ // skip (
	var bases []string
	depth := 1

	for !s.eof() && depth > 0 {
		s.skipBaseWhitespace()
		if s.eof() {
			break
		}
		ch := s.src[s.pos]
		if ch == ')' {
			depth--
			s.pos++
			break
		}
		if ch == ',' {
			s.pos++
			continue
		}
		// Read base class name (possibly dotted, possibly with type params)
		if pyIsIdentStart(ch) {
			name := s.readWord()
			// Check for dotted name
			for s.pos < len(s.src) && s.src[s.pos] == '.' {
				s.pos++
				name += "." + s.readWord()
			}
			// Check for keyword arg (metaclass=, etc.) - skip these
			s.skipBaseWhitespace()
			if s.pos < len(s.src) && s.src[s.pos] == '=' {
				s.pos++
				// Skip the value
				bdepth := 0
				for s.pos < len(s.src) {
					c := s.src[s.pos]
					if c == '(' || c == '[' {
						bdepth++
					} else if c == ')' || c == ']' {
						if bdepth == 0 {
							break
						}
						bdepth--
					} else if c == ',' && bdepth == 0 {
						break
					} else if c == '\n' {
						s.line++
					}
					s.pos++
				}
				continue
			}
			// Skip type parameters like Generic[T]
			if s.pos < len(s.src) && s.src[s.pos] == '[' {
				bracketStart := s.pos
				bdepth := 1
				s.pos++
				for s.pos < len(s.src) && bdepth > 0 {
					if s.src[s.pos] == '[' {
						bdepth++
					} else if s.src[s.pos] == ']' {
						bdepth--
					} else if s.src[s.pos] == '\n' {
						s.line++
					}
					s.pos++
				}
				name += s.src[bracketStart:s.pos]
			}
			bases = append(bases, name)
		} else {
			s.pos++
		}
	}
	return bases
}

func (s *pyScanner) skipBaseWhitespace() {
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			s.pos++
		} else if ch == '\n' {
			s.line++
			s.pos++
		} else {
			return
		}
	}
}

func (s *pyScanner) finishClass(shape *FileShape) {
	if s.currentClass != nil {
		shape.Types = append(shape.Types, *s.currentClass)
	}
	s.classIndent = -1
	s.currentClass = nil
}

// Function parsing

func (s *pyScanner) parseFuncDef(shape *FileShape, class *TypeDef, indent int) {
	line := s.line
	s.readWord() // consume "def"
	s.skipInlineWhitespace()
	name := s.readWord()
	if name == "" {
		s.skipToNextLine()
		return
	}

	s.skipInlineWhitespace()
	sig := s.readSignature()

	if s.exportedOnly && pyIsPrivate(name) {
		s.skipIndent = indent
		s.skipToNextLine()
		return
	}

	fd := FuncDef{
		Name:      name,
		Signature: sig,
		Line:      line,
	}

	if class != nil {
		class.Methods = append(class.Methods, fd)
	} else {
		shape.Functions = append(shape.Functions, fd)
	}

	s.skipIndent = indent
	s.skipToNextLine()
}

func (s *pyScanner) parseAsyncFuncDef(shape *FileShape, class *TypeDef, indent int) {
	line := s.line
	s.readWord() // consume "def"
	s.skipInlineWhitespace()
	name := s.readWord()
	if name == "" {
		s.skipToNextLine()
		return
	}

	s.skipInlineWhitespace()
	sig := s.readSignature()

	if s.exportedOnly && pyIsPrivate(name) {
		s.skipIndent = indent
		s.skipToNextLine()
		return
	}

	fd := FuncDef{
		Name:      name,
		Signature: "async " + sig,
		Line:      line,
	}

	if class != nil {
		class.Methods = append(class.Methods, fd)
	} else {
		shape.Functions = append(shape.Functions, fd)
	}

	s.skipIndent = indent
	s.skipToNextLine()
}

func (s *pyScanner) parseDecoratedFuncDef(shape *FileShape, class *TypeDef, indent int, decorators []string) {
	line := s.line
	s.readWord() // consume "def"
	s.skipInlineWhitespace()
	name := s.readWord()
	if name == "" {
		s.skipToNextLine()
		return
	}

	s.skipInlineWhitespace()
	sig := s.readSignature()

	if s.exportedOnly && pyIsPrivate(name) {
		s.skipIndent = indent
		s.skipToNextLine()
		return
	}

	// Prepend decorators to signature
	decoStr := strings.Join(decorators, " ")
	sig = decoStr + " " + sig

	fd := FuncDef{
		Name:      name,
		Signature: sig,
		Line:      line,
	}

	if class != nil {
		class.Methods = append(class.Methods, fd)
	} else {
		shape.Functions = append(shape.Functions, fd)
	}

	s.skipIndent = indent
	s.skipToNextLine()
}

func (s *pyScanner) parseDecoratedAsyncFuncDef(shape *FileShape, class *TypeDef, indent int, decorators []string) {
	line := s.line
	s.readWord() // consume "def"
	s.skipInlineWhitespace()
	name := s.readWord()
	if name == "" {
		s.skipToNextLine()
		return
	}

	s.skipInlineWhitespace()
	sig := s.readSignature()

	if s.exportedOnly && pyIsPrivate(name) {
		s.skipIndent = indent
		s.skipToNextLine()
		return
	}

	decoStr := strings.Join(decorators, " ")
	sig = decoStr + " async " + sig

	fd := FuncDef{
		Name:      name,
		Signature: sig,
		Line:      line,
	}

	if class != nil {
		class.Methods = append(class.Methods, fd)
	} else {
		shape.Functions = append(shape.Functions, fd)
	}

	s.skipIndent = indent
	s.skipToNextLine()
}

// Variable parsing

func (s *pyScanner) parseModuleVariable(shape *FileShape, indent int) {
	line := s.line
	if !pyIsIdentStart(s.peek()) {
		s.skipToNextLine()
		return
	}
	name := s.readWord()
	if name == "" {
		s.skipToNextLine()
		return
	}

	s.skipInlineWhitespace()

	// Check for type annotation: name: type [= value]
	if s.pos < len(s.src) && s.src[s.pos] == ':' {
		s.pos++ // skip :
		s.skipInlineWhitespace()

		// Read type until = or newline
		typeStr := s.readTypeAnnotation()

		if s.exportedOnly && pyIsPrivate(name) {
			s.skipToNextLine()
			return
		}

		vd := ValueDef{
			Name: name,
			Type: typeStr,
			Line: line,
		}

		s.skipInlineWhitespace()
		hasAssignment := false
		if s.pos < len(s.src) && s.src[s.pos] == '=' {
			hasAssignment = true
			s.pos++
			s.skipInlineWhitespace()
			vd.Value = s.peekSimpleValue()
		}

		shape.Variables = append(shape.Variables, vd)
		if hasAssignment {
			s.skipRHSValue()
		}
		s.skipToNextLine()
		return
	}

	// Check for assignment: name = value
	if s.pos < len(s.src) && s.src[s.pos] == '=' {
		// Make sure it's not ==
		if s.pos+1 < len(s.src) && s.src[s.pos+1] == '=' {
			s.skipToNextLine()
			return
		}
		s.pos++ // skip =
		s.skipInlineWhitespace()

		if s.exportedOnly && pyIsPrivate(name) {
			s.skipRHSValue()
			s.skipToNextLine()
			return
		}

		vd := ValueDef{
			Name:  name,
			Value: s.peekSimpleValue(),
			Line:  line,
		}

		shape.Variables = append(shape.Variables, vd)
		s.skipRHSValue()
		s.skipToNextLine()
		return
	}

	// Not a variable declaration
	s.skipToNextLine()
}

func (s *pyScanner) parseClassVariable(indent int) {
	if s.currentClass == nil {
		s.skipToNextLine()
		return
	}
	if !pyIsIdentStart(s.peek()) {
		s.skipToNextLine()
		return
	}

	name := s.readWord()
	if name == "" {
		s.skipToNextLine()
		return
	}

	s.skipInlineWhitespace()

	// Check for type annotation: name: type [= value]
	if s.pos < len(s.src) && s.src[s.pos] == ':' {
		s.pos++ // skip :
		s.skipInlineWhitespace()
		typeStr := s.readTypeAnnotation()

		if s.exportedOnly && pyIsPrivate(name) {
			s.skipToNextLine()
			return
		}

		fd := FieldDef{
			Name: name,
			Type: typeStr,
		}
		s.currentClass.Fields = append(s.currentClass.Fields, fd)
		s.skipToNextLine()
		return
	}

	// Check for assignment: name = value
	if s.pos < len(s.src) && s.src[s.pos] == '=' {
		if s.pos+1 < len(s.src) && s.src[s.pos+1] == '=' {
			s.skipToNextLine()
			return
		}

		if s.exportedOnly && pyIsPrivate(name) {
			s.skipToNextLine()
			return
		}

		fd := FieldDef{
			Name: name,
		}
		s.currentClass.Fields = append(s.currentClass.Fields, fd)
		s.skipToNextLine()
		return
	}

	s.skipToNextLine()
}

// skipRHSValue skips the right-hand side of an assignment (the value expression).
// This handles triple-quoted strings, regular strings, parenthesized expressions, etc.
func (s *pyScanner) skipRHSValue() {
	s.skipInlineWhitespace()
	if s.eof() || s.src[s.pos] == '\n' {
		return
	}
	ch := s.src[s.pos]

	// Check for string prefix before quote
	if pyIsIdentStart(ch) {
		savedPos := s.pos
		if s.skipStringPrefix() {
			s.skipStringAtPos()
			return
		}
		s.pos = savedPos
	}

	// Triple-quoted string
	if q, ok := s.isTripleQuote(); ok {
		s.advance()
		s.advance()
		s.advance()
		s.skipTripleQuotedString(q)
		return
	}

	// Regular string
	if ch == '"' || ch == '\'' {
		s.skipSingleQuotedString()
		return
	}

	// Otherwise, just skip to end of line
	// (for simple values like numbers, True/False, etc.)
}

// readTypeAnnotation reads a type annotation until = or newline.
func (s *pyScanner) readTypeAnnotation() string {
	start := s.pos
	bracketDepth := 0
	for s.pos < len(s.src) {
		ch := s.src[s.pos]
		if ch == '[' {
			bracketDepth++
			s.pos++
		} else if ch == ']' {
			bracketDepth--
			s.pos++
		} else if ch == '=' && bracketDepth == 0 {
			break
		} else if ch == '\n' {
			break
		} else if ch == '#' {
			break
		} else {
			s.pos++
		}
	}
	return strings.TrimSpace(s.src[start:s.pos])
}
