// Command clausecheck fails when a journey binds a scenario without implementing every clause.
//
// Go lets a struct literal leave fields out, so a generated scenario type alone cannot make a
// skipped clause a compile error. This check closes that gap in the gate, without running a
// single journey: it reads the journey sources, finds every literal of a generated scenario
// type, and compares its keys with scenarios.Fields. A positional (unkeyed) literal is refused
// too, because it would bind clauses by order and survive a reordering of the document.
//
// Usage: go run ./tools/clausecheck [dir ...]   (default: journeys)
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/scenarios"
)

func main() {
	dirs := os.Args[1:]
	if len(dirs) == 0 {
		dirs = []string{"journeys"}
	}
	var problems []string
	literals := 0
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			found, p, err := checkFile(path)
			literals += found
			problems = append(problems, p...)
			return err
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "clausecheck: %v\n", err)
			os.Exit(1)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr, "clausecheck: %d problem(s)\n", len(problems))
		os.Exit(1)
	}
	fmt.Printf("clausecheck: %d scenario binding(s), every clause implemented\n", literals)
}

// checkFile returns how many scenario literals the file holds and what is wrong with them.
func checkFile(path string) (int, []string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return 0, nil, err
	}
	alias := importName(file)
	if alias == "" {
		return 0, nil, nil
	}
	found := 0
	var problems []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != alias {
			return true
		}
		want, ok := scenarios.Fields[sel.Sel.Name]
		if !ok {
			return true
		}
		found++
		pos := fset.Position(lit.Pos())
		have := map[string]bool{}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: %s must name each clause (keyed fields), not list them by position", pos, sel.Sel.Name))
				return true
			}
			if id, ok := kv.Key.(*ast.Ident); ok {
				have[id.Name] = true
			}
		}
		for _, f := range want {
			if !have[f] {
				problems = append(problems, fmt.Sprintf("%s: %s does not implement clause %s", pos, sel.Sel.Name, f))
			}
		}
		return true
	})
	return found, problems, nil
}

// importName is the local name the file uses for the scenarios package, or "" if it does not
// import it.
func importName(file *ast.File) string {
	const path = `"github.com/KyleyHarris/engineroom/internal/scenarios"`
	for _, imp := range file.Imports {
		if imp.Path.Value != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "scenarios"
	}
	return ""
}
