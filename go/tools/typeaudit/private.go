package main

import (
	"go/token"
	"go/types"
)

// auditPrivate resolves every active package-level private type root, including
// CGO-generated declarations. External named capabilities remain opaque handles.
func auditPrivate(root string) (publicReport, error) {
	var out publicReport
	err := visitCompilerPackages(root, func(pkg *types.Package, positions *token.FileSet, owned map[string]bool) {
		out.packages++
		for _, name := range pkg.Scope().Names() {
			object, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || object.Exported() {
				continue
			}
			out.types++
			if publicOpenPayload(object.Type(), owned, make(map[types.Type]bool)) {
				out.findings = append(out.findings, finding{positions.Position(object.Pos()), pkg.Path() + "." + name})
			}
		}
	})
	return out, err
}
