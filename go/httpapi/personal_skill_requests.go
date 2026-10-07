package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/userresources"
)

// Personal skill request values carry no caller-supplied ownership or file path.
// Only the JSON decoder uses raw fields; service-facing values remain concrete.
type PersonalSkillPreviewRequest struct {
	Name  string `json:"name"`
	Focus string `json:"focus"`
}

type PersonalSkillCommitRequest struct {
	Digest userresources.DraftDigest `json:"digest"`
}

var personalSkillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var personalSkillDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var errPersonalSkillRequest = errors.New("invalid personal skill request")

func personalSkillFields(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return nil, errPersonalSkillRequest
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, errPersonalSkillRequest
	}
	for key := range fields {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return nil, errPersonalSkillRequest
		}
	}
	return fields, nil
}

func personalSkillString(data json.RawMessage, limit int) (string, error) {
	var value *string
	if err := json.Unmarshal(data, &value); err != nil || value == nil || utf8.RuneCountInString(*value) > limit {
		return "", errPersonalSkillRequest
	}
	return *value, nil
}

func (v *PersonalSkillPreviewRequest) UnmarshalJSON(data []byte) error {
	fields, err := personalSkillFields(data, "name", "focus")
	if err != nil {
		return err
	}
	name, err := personalSkillString(fields["name"], 64)
	if err != nil || !personalSkillName.MatchString(name) {
		return errPersonalSkillRequest
	}
	focus := ""
	if raw, present := fields["focus"]; present {
		focus, err = personalSkillString(raw, 2000)
		if err != nil {
			return err
		}
	}
	*v = PersonalSkillPreviewRequest{Name: name, Focus: focus}
	return nil
}

func (v *PersonalSkillCommitRequest) UnmarshalJSON(data []byte) error {
	fields, err := personalSkillFields(data, "digest")
	if err != nil {
		return err
	}
	digest, err := personalSkillString(fields["digest"], 64)
	if err != nil || !personalSkillDigest.MatchString(digest) {
		return errPersonalSkillRequest
	}
	*v = PersonalSkillCommitRequest{Digest: userresources.DraftDigest(digest)}
	return nil
}
