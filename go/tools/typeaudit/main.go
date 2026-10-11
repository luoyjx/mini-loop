// Command typeaudit checks production package-level type declarations for open
// payload types. Local JSON ingress DTOs and generic constraints remain allowed.
// This syntax check does not prove data flow, external type closure or authority.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type finding struct {
	position token.Position
	name     string
}
type report struct {
	files, types int
	findings     []finding
}

func jsonNames(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "encoding/json" {
			continue
		}
		name := "json"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = true
	}
	return names
}

func openPayload(node ast.Node, jsonImports map[string]bool) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.Ident:
			if value.Name == "any" || (value.Name == "RawMessage" && jsonImports["."]) {
				found = true
			}
		case *ast.InterfaceType:
			if len(value.Methods.List) == 0 {
				found = true
			}
		case *ast.SelectorExpr:
			if name, ok := value.X.(*ast.Ident); ok && jsonImports[name.Name] && value.Sel.Name == "RawMessage" {
				found = true
			}
		}
		return true
	})
	return found
}

func audit(root string) (report, error) {
	var out report
	positions := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(positions, path, nil, parser.AllErrors)
		if err != nil {
			return err
		}
		out.files++
		imports := jsonNames(file)
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, specification := range group.Specs {
				named, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				out.types++
				// TypeParams are constraints, not retained values. The body includes all
				// fields, aliases, nested maps/slices and function/interface signatures.
				if openPayload(named.Type, imports) {
					out.findings = append(out.findings, finding{positions.Position(named.Pos()), named.Name.Name})
				}
			}
		}
		return nil
	})
	return out, err
}

func main() {
	root := flag.String("root", ".", "production Go source root (all platform files)")
	public := flag.Bool("public", false, "compiler-resolved exported type closure (active build profile)")
	private := flag.Bool("private", false, "compiler-resolved private type roots (active build profile)")
	flag.Parse()
	if *public && *private {
		fmt.Fprintln(os.Stderr, "select only one of -public or -private")
		os.Exit(2)
	}
	if *private {
		result, err := auditPrivate(*root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for _, finding := range result.findings {
			fmt.Fprintf(os.Stderr, "%s: open payload in private type %s\n", finding.position, finding.name)
		}
		fmt.Printf("Go private type audit: %d packages, %d private root types, %d findings\n", result.packages, result.types, len(result.findings))
		if result.packages == 0 {
			fmt.Fprintln(os.Stderr, "no main-module packages found")
			os.Exit(2)
		}
		if len(result.findings) != 0 {
			os.Exit(1)
		}
		return
	}
	if *public {
		result, err := auditPublic(*root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for _, finding := range result.findings {
			fmt.Fprintf(os.Stderr, "%s: open payload in public type %s\n", finding.position, finding.name)
		}
		fmt.Printf("Go public type audit: %d packages, %d exported types, %d findings\n", result.packages, result.types, len(result.findings))
		if result.packages == 0 {
			fmt.Fprintln(os.Stderr, "no main-module packages found")
			os.Exit(2)
		}
		if len(result.findings) != 0 {
			os.Exit(1)
		}
		return
	}
	result, err := audit(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, finding := range result.findings {
		fmt.Fprintf(os.Stderr, "%s: open payload in type %s\n", finding.position, finding.name)
	}
	fmt.Printf("Go type audit: %d production files, %d package-level types, %d findings\n", result.files, result.types, len(result.findings))
	if result.files == 0 {
		fmt.Fprintln(os.Stderr, "no production Go files found")
		os.Exit(2)
	}
	if len(result.findings) != 0 {
		os.Exit(1)
	}
}
