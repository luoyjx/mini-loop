package launcher

import (
	"github.com/luoyjx/mini-loop/go/astcontext"
	"github.com/luoyjx/mini-loop/go/config"
)

// Settings are operator-owned. No executable lookup/probe is performed during
// construction or inspection; integrity is rechecked at each real invocation.
func configuredAST(settings config.Settings) (*astcontext.Config, error) {
	if !settings.ASTOutlineEnabled {
		return nil, nil
	}
	parameters := astcontext.DefaultParameters()
	parameters.Binary = settings.ASTOutlineBinary
	parameters.ExpectedSHA256 = settings.ASTOutlineSHA256
	parameters.TimeoutSeconds = float64(settings.ASTOutlineTimeout)
	parameters.MaxOutputBytes = settings.ASTOutlineMaxOutputBytes
	ast, err := astcontext.NewConfig(parameters)
	if err != nil {
		return nil, err
	}
	return &ast, nil
}
