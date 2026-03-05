package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

type GoExtractor struct{}

func (e *GoExtractor) Extensions() []string {
	return []string{".go"}
}

func (e *GoExtractor) Extract(filePath string, exportedOnly bool) (*FileShape, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	shape := &FileShape{
		File:    filePath,
		Package: file.Name.Name,
	}

	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
			path = imp.Name.Name + " " + path
		}
		shape.Imports = append(shape.Imports, path)
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			goExtractGenDecl(fset, d, shape, exportedOnly)
		case *ast.FuncDecl:
			if exportedOnly && !d.Name.IsExported() {
				continue
			}
			fd := FuncDef{
				Name:      d.Name.Name,
				Signature: goFuncSignature(fset, d.Type),
				Line:      fset.Position(d.Pos()).Line,
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				fd.Receiver = goExprString(fset, d.Recv.List[0].Type)
			}
			shape.Functions = append(shape.Functions, fd)
		}
	}

	return shape, nil
}

func goExtractGenDecl(fset *token.FileSet, d *ast.GenDecl, shape *FileShape, exportedOnly bool) {
	switch d.Tok {
	case token.TYPE:
		for _, spec := range d.Specs {
			ts := spec.(*ast.TypeSpec)
			if exportedOnly && !ts.Name.IsExported() {
				continue
			}
			shape.Types = append(shape.Types, goExtractType(fset, ts))
		}
	case token.CONST:
		goExtractValues(fset, d, &shape.Constants, exportedOnly)
	case token.VAR:
		goExtractValues(fset, d, &shape.Variables, exportedOnly)
	}
}

func goExtractValues(fset *token.FileSet, d *ast.GenDecl, dest *[]ValueDef, exportedOnly bool) {
	for _, spec := range d.Specs {
		vs := spec.(*ast.ValueSpec)
		for i, name := range vs.Names {
			if exportedOnly && !name.IsExported() {
				continue
			}
			vd := ValueDef{
				Name: name.Name,
				Line: fset.Position(name.Pos()).Line,
			}
			if vs.Type != nil {
				vd.Type = goExprString(fset, vs.Type)
			}
			if i < len(vs.Values) {
				vd.Value = goExprString(fset, vs.Values[i])
			}
			*dest = append(*dest, vd)
		}
	}
}

func goExtractType(fset *token.FileSet, ts *ast.TypeSpec) TypeDef {
	td := TypeDef{
		Name: ts.Name.Name,
		Line: fset.Position(ts.Name.Pos()).Line,
	}

	if ts.TypeParams != nil && len(ts.TypeParams.List) > 0 {
		td.Name += "[" + goFieldListString(fset, ts.TypeParams.List) + "]"
	}

	switch t := ts.Type.(type) {
	case *ast.StructType:
		td.Kind = "struct"
		if t.Fields != nil {
			for _, f := range t.Fields.List {
				if len(f.Names) == 0 {
					td.Fields = append(td.Fields, FieldDef{
						Type: goExprString(fset, f.Type),
						Tag:  goTagString(f.Tag),
					})
				} else {
					for _, name := range f.Names {
						td.Fields = append(td.Fields, FieldDef{
							Name: name.Name,
							Type: goExprString(fset, f.Type),
							Tag:  goTagString(f.Tag),
						})
					}
				}
			}
		}
	case *ast.InterfaceType:
		td.Kind = "interface"
		if t.Methods != nil {
			for _, m := range t.Methods.List {
				if len(m.Names) > 0 {
					if ft, ok := m.Type.(*ast.FuncType); ok {
						td.Methods = append(td.Methods, FuncDef{
							Name:      m.Names[0].Name,
							Signature: goFuncSignature(fset, ft),
							Line:      fset.Position(m.Pos()).Line,
						})
					}
				} else {
					td.Embeds = append(td.Embeds, goExprString(fset, m.Type))
				}
			}
		}
	default:
		if ts.Assign.IsValid() {
			td.Kind = "alias"
		} else {
			td.Kind = "named"
		}
		td.Underlying = goExprString(fset, ts.Type)
	}

	return td
}

func goExprString(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, expr); err != nil {
		return "<error>"
	}
	return buf.String()
}

func goFuncSignature(fset *token.FileSet, ft *ast.FuncType) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, ft); err != nil {
		return "<error>"
	}
	return strings.TrimPrefix(buf.String(), "func")
}

func goFieldListString(fset *token.FileSet, fields []*ast.Field) string {
	var parts []string
	for _, f := range fields {
		typeStr := goExprString(fset, f.Type)
		if len(f.Names) > 0 {
			names := make([]string, len(f.Names))
			for i, n := range f.Names {
				names[i] = n.Name
			}
			parts = append(parts, strings.Join(names, ", ")+" "+typeStr)
		} else {
			parts = append(parts, typeStr)
		}
	}
	return strings.Join(parts, ", ")
}

func goTagString(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	return tag.Value
}
