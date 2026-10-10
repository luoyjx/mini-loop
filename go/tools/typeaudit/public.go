package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
)

type moduleMetadata struct {
	Path string
	Main bool
}

type packageMetadata struct {
	ImportPath string
	Export     string
	Module     *moduleMetadata
}

type publicReport struct {
	packages, types int
	findings        []finding
}

// auditPublic uses compiler export data for the active build profile. Private
// types are traversed when reachable from exported types; unexported roots and
// inactive platform definitions remain the syntax audit's separate scope.
func auditPublic(root string) (publicReport, error) {
	var out publicReport
	command := exec.Command("go", "list", "-export", "-deps", "-json", "./...")
	command.Dir = root
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	data, err := command.Output()
	if err != nil {
		return out, fmt.Errorf("compiler package inventory: %w: %s", err, diagnostics.String())
	}
	var packages []packageMetadata
	exports := make(map[string]string)
	owned := make(map[string]bool)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var pkg packageMetadata
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return out, err
		}
		exports[pkg.ImportPath] = pkg.Export
		if pkg.Module != nil && pkg.Module.Main {
			packages = append(packages, pkg)
			owned[pkg.ImportPath] = true
		}
	}
	positions := token.NewFileSet()
	compiler := importer.ForCompiler(positions, "gc", func(path string) (io.ReadCloser, error) {
		if exports[path] == "" {
			return nil, fmt.Errorf("missing compiler export for %s", path)
		}
		return os.Open(exports[path])
	})
	for _, metadata := range packages {
		pkg, err := compiler.Import(metadata.ImportPath)
		if err != nil {
			return out, err
		}
		out.packages++
		for _, name := range pkg.Scope().Names() {
			object, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || !object.Exported() {
				continue
			}
			out.types++
			if publicOpenPayload(object.Type(), owned, make(map[types.Type]bool)) {
				out.findings = append(out.findings, finding{positions.Position(object.Pos()), metadata.ImportPath + "." + name})
			}
		}
	}
	return out, nil
}

func publicOpenPayload(value types.Type, owned map[string]bool, seen map[types.Type]bool) bool {
	value = types.Unalias(value)
	if seen[value] {
		return false
	}
	seen[value] = true
	visit := func(value types.Type) bool { return publicOpenPayload(value, owned, seen) }
	switch value := value.(type) {
	case *types.Named:
		object := value.Obj()
		if object.Pkg() != nil && object.Pkg().Path() == "encoding/json" && object.Name() == "RawMessage" {
			return true
		}
		for index := 0; index < value.TypeArgs().Len(); index++ {
			if visit(value.TypeArgs().At(index)) {
				return true
			}
		}
		// External structs and named capabilities are opaque platform handles.
		// Aliases, erased interfaces and containers still expose their payload.
		if object.Pkg() != nil && !owned[object.Pkg().Path()] {
			switch underlying := value.Underlying().(type) {
			case *types.Struct:
				return false
			case *types.Interface:
				if underlying.NumMethods() != 0 {
					return false
				}
			}
		}
		return visit(value.Underlying())
	case *types.Struct:
		for index := 0; index < value.NumFields(); index++ {
			if visit(value.Field(index).Type()) {
				return true
			}
		}
	case *types.Interface:
		if !value.IsMethodSet() {
			return false // constraint, not a retained value
		}
		if value.NumMethods() == 0 {
			return true
		}
		for index := 0; index < value.NumMethods(); index++ {
			if visit(value.Method(index).Type()) {
				return true
			}
		}
	case *types.Pointer:
		return visit(value.Elem())
	case *types.Slice:
		return visit(value.Elem())
	case *types.Array:
		return visit(value.Elem())
	case *types.Map:
		return visit(value.Key()) || visit(value.Elem())
	case *types.Chan:
		return visit(value.Elem())
	case *types.Signature:
		return visit(value.Params()) || visit(value.Results())
	case *types.Tuple:
		for index := 0; index < value.Len(); index++ {
			if visit(value.At(index).Type()) {
				return true
			}
		}
	}
	return false
}
