package memory

import (
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type bodyKind uint8

const (
	bodyText bodyKind = iota
	bodyContainer
	bodyUnsized
)

// Body is an immutable projection of a model body at its consumption boundary.
// It retains only text, original length and an explicit kind, never JSON data.
// The zero value is an empty text body. Stores apply the projection at write time.
type Body struct {
	kind   bodyKind
	text   string
	length int
}

func TextBody(text string) Body { return Body{kind: bodyText, text: text} }

// ContainerBody keeps Python's original element count separate from repr length.
// Oversized containers report truncation and fail at write time; repr is unused.
func ContainerBody(rendered string, elements int) (Body, error) {
	if elements < 0 {
		return Body{}, errors.New("memory container length must be nonnegative")
	}
	return Body{kind: bodyContainer, text: rendered, length: elements}, nil
}

// UnsizedBody defers the scalar len failure to the store's write phase.
func UnsizedBody() Body { return Body{kind: bodyUnsized} }

// WithBody returns a copy selecting an immutable observation instead of the
// ordinary Input.Body string field. This changes no owner, path or authority.
// It also selects Source header encoding phases: fallback name hashing before
// body processing; authenticated name hashing and final encoding after masking.
func (input Input) WithBody(body Body) Input {
	input.Body = ""
	input.body = &body
	return input
}

func (body Body) render() (text string, originalLength int, truncated bool, err error) {
	switch body.kind {
	case bodyUnsized:
		return "", 0, false, errors.New("memory body has no length")
	case bodyContainer:
		if body.length > MaxBody {
			return "", body.length, true, errors.New("memory container body cannot be truncated")
		}
		return body.text, body.length, false, nil
	default:
		length := jsonvalue.RuneCount(body.text)
		if length > MaxBody {
			return jsonvalue.TextPrefix(body.text, MaxBody) + "\n[memory truncated]", length, true, nil
		}
		return body.text, length, false, nil
	}
}
