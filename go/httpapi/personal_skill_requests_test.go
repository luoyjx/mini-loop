package httpapi

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/luoyjx/mini-loop/go/userresources"
)

func TestActualPythonPersonalSkillRequestContracts(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-personal-skill-requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind     string `json:"kind"`
			Name     string `json:"name"`
			Raw      string `json:"raw"`
			Accepted bool   `json:"accepted"`
			Result   *struct {
				Name   string                    `json:"name"`
				Focus  string                    `json:"focus"`
				Digest userresources.DraftDigest `json:"digest"`
			} `json:"result"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 43 {
		t.Fatalf("expected 43 actual source cases, got %d", len(fixture.Cases))
	}
	for _, c := range fixture.Cases {
		t.Run(c.Kind+"/"+c.Name, func(t *testing.T) {
			switch c.Kind {
			case "preview":
				var got PersonalSkillPreviewRequest
				err := json.Unmarshal([]byte(c.Raw), &got)
				if (err == nil) != c.Accepted {
					t.Fatalf("acceptance: got %v, source %v", err, c.Accepted)
				}
				if c.Accepted && (got.Name != c.Result.Name || got.Focus != c.Result.Focus) {
					t.Fatalf("normalized request differs: %#v", got)
				}
			case "commit":
				var got PersonalSkillCommitRequest
				err := json.Unmarshal([]byte(c.Raw), &got)
				if (err == nil) != c.Accepted {
					t.Fatalf("acceptance: got %v, source %v", err, c.Accepted)
				}
				if c.Accepted && got.Digest != c.Result.Digest {
					t.Fatalf("normalized digest differs: %q", got.Digest)
				}
			default:
				t.Fatalf("unknown fixture kind %q", c.Kind)
			}
		})
	}
}

func TestPersonalSkillRequestFailurePreservesTypedReceiver(t *testing.T) {
	preview := PersonalSkillPreviewRequest{Name: "previous", Focus: "reviewed"}
	if err := json.Unmarshal([]byte(`{"name":"safe","focus":null}`), &preview); err == nil {
		t.Fatal("null focus admitted")
	}
	if preview != (PersonalSkillPreviewRequest{Name: "previous", Focus: "reviewed"}) {
		t.Fatal("failed decode partially changed receiver")
	}
	commit := PersonalSkillCommitRequest{Digest: "previous"}
	if err := json.Unmarshal([]byte(`{"digest":"bad"}`), &commit); err == nil || commit.Digest != "previous" {
		t.Fatal("failed commit decode changed receiver")
	}
}
