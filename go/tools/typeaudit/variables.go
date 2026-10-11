package main

import (
	"go/token"
	"go/types"
)

type variableReport struct {
	packages, variables int
	findings            []finding
}

// auditVariables includes exported and private package variables, including
// inferred declarations and instantiated generic containers. Constants, blank
// identifiers and function locals are not retained package variable roots.
func auditVariables(root string) (variableReport, error) {
	var out variableReport
	err := visitCompilerPackages(root, func(pkg *types.Package, positions *token.FileSet, owned map[string]bool) {
		out.packages++
		for _, name := range pkg.Scope().Names() {
			object, ok := pkg.Scope().Lookup(name).(*types.Var)
			if !ok {
				continue
			}
			out.variables++
			if publicOpenPayload(object.Type(), owned, make(map[types.Type]bool)) {
				out.findings = append(out.findings, finding{positions.Position(object.Pos()), pkg.Path() + "." + name})
			}
		}
	})
	return out, err
}
