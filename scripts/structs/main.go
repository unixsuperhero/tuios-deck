// Command structs prints every struct type of the deck as one Markdown table
// per package: the struct, how many fields it has, whether it is serialised
// (any field carries a json tag), where it is defined, and the first
// sentence of its doc comment. Run from the repository root:
//
//	GOWORK=off go run ./scripts/structs
//
// It is the struct extractor of docs/plans/2026-10-18-feature-inventory.md,
// stage 1, and docs/reports/2026-10-18-modular-redesign.md embeds its output.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type row struct {
	name, where, doc string
	fields           int
	json, exported   bool
}

func main() {
	byPkg := map[string][]row{}
	fset := token.NewFileSet()
	for _, root := range []string{"cmd", "internal", "setup"} {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return nil
			}
			pkg := filepath.Dir(path)
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					doc := ts.Doc
					if doc == nil {
						doc = gd.Doc
					}
					r := row{name: ts.Name.Name, exported: ts.Name.IsExported(), doc: firstSentence(doc.Text())}
					pos := fset.Position(ts.Pos())
					r.where = fmt.Sprintf("%s:%d", filepath.Base(pos.Filename), pos.Line)
					for _, fl := range st.Fields.List {
						if n := len(fl.Names); n > 0 {
							r.fields += n
						} else {
							r.fields++ // embedded
						}
						if fl.Tag != nil && strings.Contains(fl.Tag.Value, "json:") {
							r.json = true
						}
					}
					byPkg[pkg] = append(byPkg[pkg], r)
				}
			}
			return nil
		})
	}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	total := 0
	for _, p := range pkgs {
		total += len(byPkg[p])
	}
	fmt.Printf("%d structs in %d packages. Fields counts embedded types as one field each; JSON says whether any field carries a `json` tag, which is what the store writes or the web page reads.\n\n", total, len(pkgs))
	for _, p := range pkgs {
		rows := byPkg[p]
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].exported != rows[j].exported {
				return rows[i].exported
			}
			return rows[i].name < rows[j].name
		})
		fmt.Printf("### %s (%d)\n\n| Struct | Fields | JSON | Where | Purpose |\n|---|---:|:---:|---|---|\n", p, len(rows))
		for _, r := range rows {
			name := "`" + r.name + "`"
			if !r.exported {
				name += " ·"
			}
			j := ""
			if r.json {
				j = "✓"
			}
			fmt.Printf("| %s | %d | %s | `%s` | %s |\n", name, r.fields, j, r.where, strings.ReplaceAll(r.doc, "|", "\\|"))
		}
		fmt.Println()
	}
	fmt.Println("A `·` after a name marks an unexported struct.")
}

// firstSentence is the doc comment up to its first full stop, on one line.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	return s
}
