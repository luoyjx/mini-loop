package main

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
)

// auditPrivate resolves every active package-level private type root, including
// CGO-generated declarations, without evaluating bodies or executing code.
// External named capabilities retain the public audit's opaque-handle boundary.
func auditPrivate(root string) (publicReport, error) {
	var out publicReport
	inventory, err := inventoryCompilerPackages(root, true)
	if err != nil {
		return out, err
	}
	positions := token.NewFileSet()
	compiler := importer.ForCompiler(positions, "gc", func(path string) (io.ReadCloser, error) {
		if inventory.exports[path] == "" {
			return nil, fmt.Errorf("missing compiler export for %s", path)
		}
		return os.Open(inventory.exports[path])
	})
	for _, metadata := range inventory.packages {
		if len(metadata.CompiledGoFiles) == 0 {
			return out, fmt.Errorf("no compiled source files for %s", metadata.ImportPath)
		}
		var files []*ast.File
		for _, path := range metadata.CompiledGoFiles {
			if !filepath.IsAbs(path) {
				path = filepath.Join(metadata.Dir, path)
			}
			file, err := parser.ParseFile(positions, path, nil, parser.AllErrors)
			if err != nil {
				return out, fmt.Errorf("private type source %s: %w", metadata.ImportPath, err)
			}
			files = append(files, file)
		}
		config := types.Config{Importer: compiler, IgnoreFuncBodies: true}
		pkg, err := config.Check(metadata.ImportPath, positions, files, nil)
		if err != nil {
			return out, fmt.Errorf("private type resolution %s: %w", metadata.ImportPath, err)
		}
		out.packages++
		for _, name := range pkg.Scope().Names() {
			object, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || object.Exported() {
				continue
			}
			out.types++
			if publicOpenPayload(object.Type(), inventory.owned, make(map[types.Type]bool)) {
				out.findings = append(out.findings, finding{positions.Position(object.Pos()), metadata.ImportPath + "." + name})
			}
		}
	}
	return out, nil
}
