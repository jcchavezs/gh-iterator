// Package godocmd renders the documentation of a Go package as markdown.
package godocmd

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Render returns the markdown documentation of the exported API of the package in dir.
// Test files are ignored. Headings start at level 4 so the output can be nested in a document.
func Render(dir, importPath string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return "", err
		}
		files = append(files, f)
	}

	p, err := doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return "", err
	}

	r := renderer{fset: fset}
	r.text(p.Doc)
	r.values(p.Consts)
	r.values(p.Vars)
	r.funcs("func", p.Funcs)
	for _, t := range p.Types {
		r.heading("type " + t.Name)
		r.code(t.Decl)
		r.text(t.Doc)
		r.values(t.Consts)
		r.values(t.Vars)
		r.funcs("func", t.Funcs)
		r.funcs("method", t.Methods)
	}

	return strings.TrimSpace(r.sb.String()), r.err
}

type renderer struct {
	fset *token.FileSet
	sb   strings.Builder
	err  error
}

func (r *renderer) heading(s string) {
	fmt.Fprintf(&r.sb, "#### %s\n\n", s)
}

func (r *renderer) text(s string) {
	if s = strings.TrimSpace(s); s != "" {
		_, _ = r.sb.WriteString(s + "\n\n")
	}
}

// code prints the declaration without its doc comment, which is rendered as text.
func (r *renderer) code(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.GenDecl:
		c := *d
		c.Doc = nil
		decl = &c
	case *ast.FuncDecl:
		c := *d
		c.Doc, c.Body = nil, nil
		decl = &c
	}

	var buf bytes.Buffer
	if err := (&printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}).Fprint(&buf, r.fset, decl); err != nil && r.err == nil {
		r.err = err
	}
	fmt.Fprintf(&r.sb, "```go\n%s\n```\n\n", buf.String())
}

func (r *renderer) values(vs []*doc.Value) {
	for _, v := range vs {
		r.code(v.Decl)
		r.text(v.Doc)
	}
}

func (r *renderer) funcs(kind string, fs []*doc.Func) {
	for _, f := range fs {
		title := kind + " " + f.Name
		if f.Recv != "" {
			title = "method (" + f.Recv + ") " + f.Name
		}
		r.heading(title)
		r.code(f.Decl)
		r.text(f.Doc)
	}
}
