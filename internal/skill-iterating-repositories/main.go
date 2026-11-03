// Command skill-iterating-repositories regenerates the generated sections of the
// iterating-repositories skill: the godoc of the public packages and the examples table.
//
// Run it from the root of the repository (see `make generate-skill`).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jcchavezs/gh-iterator/internal/godocmd"
)

const (
	modulePath  = "github.com/jcchavezs/gh-iterator"
	blobBaseURL = "https://github.com/jcchavezs/gh-iterator/blob/main/"
)

// publicPackages are the packages documented in the skill, relative to the repository root.
var publicPackages = []string{".", "github", "exec"}

func main() {
	skillFile := flag.String("file", ".claude/skills/iterating-repositories/SKILL.md", "skill file to update")
	examplesDir := flag.String("examples", "examples", "directory with the examples")
	flag.Parse()

	if err := run(*skillFile, *examplesDir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(skillFile, examplesDir string) error {
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return err
	}

	godoc, err := generateGodoc()
	if err != nil {
		return fmt.Errorf("generating godoc: %w", err)
	}

	examples, err := generateExamplesTable(examplesDir)
	if err != nil {
		return fmt.Errorf("generating examples table: %w", err)
	}

	content, err = replaceSection(content, "Godoc", godoc)
	if err != nil {
		return err
	}

	content, err = replaceSection(content, "Examples table", examples)
	if err != nil {
		return err
	}

	return os.WriteFile(skillFile, content, 0o644)
}

// replaceSection replaces what is between `<!-- Generated: <name> begins -->` and
// `<!-- Generated: <name> ends -->` with body.
func replaceSection(content []byte, name, body string) ([]byte, error) {
	begin := []byte(fmt.Sprintf("<!-- Generated: %s begins -->", name))
	end := []byte(fmt.Sprintf("<!-- Generated: %s ends -->", name))

	b := bytes.Index(content, begin)
	if b < 0 {
		return nil, fmt.Errorf("marker %q not found", begin)
	}
	b += len(begin)

	e := bytes.Index(content[b:], end)
	if e < 0 {
		return nil, fmt.Errorf("marker %q not found after %q", end, begin)
	}
	e += b

	var out bytes.Buffer
	out.Write(content[:b])
	out.WriteString("\n" + strings.TrimSpace(body) + "\n")
	out.Write(content[e:])
	return out.Bytes(), nil
}

// generateGodoc renders the public API of every package in publicPackages.
func generateGodoc() (string, error) {
	var sb strings.Builder
	for _, pkg := range publicPackages {
		importPath := modulePath
		if pkg != "." {
			importPath = path.Join(modulePath, pkg)
		}

		md, err := godocmd.Render(pkg, importPath)
		if err != nil {
			return "", fmt.Errorf("rendering %s: %w", pkg, err)
		}

		fmt.Fprintf(&sb, "### Package `%s`\n\n```go\nimport %q\n```\n\n%s\n\n", importPath, importPath, md)
	}

	return sb.String(), nil
}

// generateExamplesTable builds a markdown table with the path of every example and the
// package comment found in its doc.go.
func generateExamplesTable(examplesDir string) (string, error) {
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("| Example | Shows |\n|---|---|\n")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(examplesDir, entry.Name())
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "doc.go"), nil, parser.ParseComments|parser.PackageClauseOnly)
		if err != nil {
			return "", err
		}
		if f.Doc == nil {
			return "", fmt.Errorf("%s/doc.go has no package comment", dir)
		}

		mainPath := filepath.ToSlash(filepath.Join(dir, "main.go"))
		shows := strings.Join(strings.Fields(f.Doc.Text()), " ")
		fmt.Fprintf(&sb, "| [%s](%s%s) | %s |\n", mainPath, blobBaseURL, mainPath, strings.ReplaceAll(shows, "|", "\\|"))
	}

	return sb.String(), nil
}
