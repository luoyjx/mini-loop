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

// visitCompilerPackages resolves active compiler-selected sources, including CGO
// declarations. Function bodies are not inspected and no package code is run.
func visitCompilerPackages(root string, visit func(*types.Package, *token.FileSet, map[string]bool)) error {
	inventory, err := inventoryCompilerPackages(root, true)
	if err != nil {
		return err
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
			return fmt.Errorf("no compiled source files for %s", metadata.ImportPath)
		}
		var files []*ast.File
		for _, path := range metadata.CompiledGoFiles {
			if !filepath.IsAbs(path) {
				path = filepath.Join(metadata.Dir, path)
			}
			file, err := parser.ParseFile(positions, path, nil, parser.AllErrors)
			if err != nil {
				return fmt.Errorf("compiler source %s: %w", metadata.ImportPath, err)
			}
			files = append(files, file)
		}
		config := types.Config{Importer: compiler, IgnoreFuncBodies: true}
		pkg, err := config.Check(metadata.ImportPath, positions, files, nil)
		if err != nil {
			return fmt.Errorf("compiler type resolution %s: %w", metadata.ImportPath, err)
		}
		visit(pkg, positions, inventory.owned)
	}
	return nil
}
