package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

// Compiler inventory is temporary inspection data, never a runtime payload.
type compilerInventory struct {
	packages []packageMetadata
	exports  map[string]string
	owned    map[string]bool
}

func inventoryCompilerPackages(root string, compiled bool) (compilerInventory, error) {
	out := compilerInventory{exports: make(map[string]string), owned: make(map[string]bool)}
	args := []string{"list", "-export", "-deps", "-json"}
	if compiled {
		args = append(args, "-compiled")
	}
	args = append(args, "./...")
	command := exec.Command("go", args...)
	command.Dir = root
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	data, err := command.Output()
	if err != nil {
		return out, fmt.Errorf("compiler package inventory: %w: %s", err, diagnostics.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var pkg packageMetadata
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return out, err
		}
		out.exports[pkg.ImportPath] = pkg.Export
		if pkg.Module != nil && pkg.Module.Main {
			out.packages = append(out.packages, pkg)
			out.owned[pkg.ImportPath] = true
		}
	}
	return out, nil
}
