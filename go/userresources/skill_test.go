package userresources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
func TestActualPythonCanonicalSkillsAndOwnerKeys(t *testing.T) {
	raw, e := os.ReadFile("../testdata/python-user-skills.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		Cases []struct {
			Input struct {
				Name, Description, Body string
				DescriptionRepeat       int `json:"description_repeat"`
				BodyRepeat              int `json:"body_repeat"`
			}
			TextSHA256  string `json:"text_sha256"`
			Description string
			BodySHA256  string         `json:"body_sha256"`
			ErrorCode   ValidationCode `json:"error_code"`
			Error       string
		}
		Owners []struct {
			Owner string
			Key   DirectoryKey
		}
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	for i, row := range fixture.Cases {
		fields := SkillFields{row.Input.Name, row.Input.Description, row.Input.Body}
		if row.Input.DescriptionRepeat > 0 {
			fields.Description = strings.Repeat(fields.Description, row.Input.DescriptionRepeat)
		}
		if row.Input.BodyRepeat > 0 {
			fields.Body = strings.Repeat(fields.Body, row.Input.BodyRepeat)
		}
		canonical, e := NewCanonicalSkill(fields)
		if row.ErrorCode != "" {
			var problem *ValidationError
			if !errors.As(e, &problem) || problem.Code() != row.ErrorCode || problem.Error() != row.Error {
				t.Fatalf("case %d: %v, want %s %s", i, e, row.ErrorCode, row.Error)
			}
			continue
		}
		if e != nil {
			t.Fatalf("case %d: %v", i, e)
		}
		normalized := canonical.Fields()
		if canonical.Digest() != row.TextSHA256 || digest(canonical.Text()) != row.TextSHA256 || normalized.Description != row.Description || digest(normalized.Body) != row.BodySHA256 {
			t.Fatal(i, "canonical fields differ")
		}
		normalized.Body = "changed"
		if canonical.Fields().Body == normalized.Body {
			t.Fatal("accessor aliases snapshot")
		}
	}
	seen := map[DirectoryKey]bool{}
	for _, row := range fixture.Owners {
		key, e := OwnerDirectoryKey(row.Owner)
		if e != nil || key != row.Key || seen[key] {
			t.Fatal(row.Owner, key, e)
		}
		seen[key] = true
	}
}
func TestClosedSkillUTF8AndEmptyOwnerRefusal(t *testing.T) {
	for _, owner := range []string{"", string([]byte{255})} {
		if _, e := OwnerDirectoryKey(owner); e == nil {
			t.Fatal("invalid owner accepted")
		}
	}
	for _, fields := range []SkillFields{{"note", string([]byte{255}), "body"}, {"note", "description", string([]byte{255})}} {
		var problem *ValidationError
		_, e := NewCanonicalSkill(fields)
		if !errors.As(e, &problem) || problem.Code() != UnsafeContent {
			t.Fatal(e)
		}
	}
}
