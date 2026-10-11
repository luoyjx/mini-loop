package improvement

import "github.com/luoyjx/mini-loop/go/internal/jsonvalue"

// ArchiveValue retains the public historical archive projection. Parsing and
// escaping are shared with other Python JSONL stores without open Go objects.
type ArchiveValue = jsonvalue.Value
type ArchiveValueKind = jsonvalue.Kind

const (
	ArchiveNull    = jsonvalue.Null
	ArchiveText    = jsonvalue.Text
	ArchiveInteger = jsonvalue.Integer
	ArchiveFloat   = jsonvalue.Float
	ArchiveBoolean = jsonvalue.Boolean
	ArchiveArray   = jsonvalue.Array
	ArchiveObject  = jsonvalue.Object
)

var errArchiveSyntax = jsonvalue.ErrSyntax
var ErrArchiveInteger = jsonvalue.ErrInteger
var ErrArchiveDepth = jsonvalue.ErrDepth
var ErrArchiveSurrogate = jsonvalue.ErrSurrogate
var ErrArchiveNonfinite = jsonvalue.ErrNonfinite

func decodeArchiveValue(data string) (ArchiveValue, error) { return jsonvalue.Decode(data) }
func appendArchiveValue(out []byte, v ArchiveValue, allowLegacy bool) ([]byte, error) {
	if allowLegacy {
		return jsonvalue.AppendLegacy(out, v)
	}
	data, err := v.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return append(out, data...), nil
}
