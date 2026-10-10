package protocol

import (
	"encoding/json"
	"os"
	"testing"
)

func TestNonfiniteBashVariantsRetainArchiveIdentityAndStrictCallerBoundary(t *testing.T) {
	assertNonfiniteShellArchives(t, "../testdata/python-provider-nonfinite-bash.json", ToolBash, 24)
}

func TestNonfiniteBackgroundVariantsRetainArchiveIdentityAndStrictCallerBoundary(t *testing.T) {
	assertNonfiniteShellArchives(t, "../testdata/python-provider-nonfinite-background.json", ToolBackgroundRun, 18)
}

func assertNonfiniteShellArchives(t *testing.T, path string, name ToolName, wantCount int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Input      string `json:"input_json"`
			Canonical  string `json:"canonical_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range fixture.Cases {
		if row.Mode != "keep" {
			continue
		}
		count++
		t.Run(row.Name, func(t *testing.T) {
			input, err := DecodeModelToolInput(name, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := input.CanonicalJSON()
			if err != nil || canonical != row.Canonical {
				t.Fatal("Source identity changed", canonical, row.Canonical, err)
			}
			if _, err := DecodeToolInput(name, []byte(row.Input)); err == nil {
				t.Fatal("strict caller admitted SDK nonfinite input")
			}
			if _, err := json.Marshal(input); err == nil {
				t.Fatal("finite encoding admitted observation")
			}
			for _, value := range []ToolInput{input, input.clone(), MapToolInputStrings(input, func(s string) string { return s })} {
				message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUseWithCaller("call", value, nil))}
				data, err := message.MarshalArchiveJSON()
				if err != nil {
					t.Fatal(err)
				}
				restored, err := DecodeStoredMessage(data)
				if err != nil {
					t.Fatal(err)
				}
				blocks, _ := restored.Content.Blocks()
				use, _ := blocks[0].ToolUse()
				actual, err := use.Input.CanonicalJSON()
				if err != nil || actual != row.Canonical {
					t.Fatal("archive changed immutable input", actual, row.Canonical, err)
				}
			}
		})
	}
	if count != wantCount {
		t.Fatal("Source unique input inventory changed", count)
	}
}
