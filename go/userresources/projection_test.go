package userresources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestSkillProjectionsMatchActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-skill-projection.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Messages   []struct {
				Role    protocol.Role
				Content protocol.Content
			}
			MaxChars        int `json:"max_chars"`
			Repeat, Minimum int
			Prior           int `json:"prior_omitted"`
			Compacted       bool
			Values          []string
			SHA256          string
			Count           int
			Coverage        DraftCoverage
			Omitted         int
			CompactedResult bool `json:"compacted_result"`
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 38 {
		t.Fatal("incomplete corpus", len(fixture.Cases), err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			registry := secrets.New(secrets.Config{MinLength: &row.Minimum})
			for _, value := range row.Values {
				registry.RegisterValue(secrets.Name(value), value)
			}
			var projected SkillProjection
			var err error
			if row.Mode == "authenticated" {
				messages := make([]AuthenticatedText, 0, len(row.Messages))
				for _, message := range row.Messages {
					plain, ok := message.Content.Plain()
					if !ok {
						t.Fatal("invalid authenticated recipe")
					}
					messages = append(messages, AuthenticatedText{message.Role, strings.Repeat(plain, row.Repeat)})
				}
				projected, err = ProjectAuthenticatedText(messages, registry, ProjectionOptions{MaxChars: row.MaxChars, PriorOmitted: row.Prior, CompactedHistoryExcluded: row.Compacted})
			} else {
				messages := make([]protocol.Message, 0, len(row.Messages))
				for _, message := range row.Messages {
					content := message.Content
					if plain, ok := content.Plain(); ok {
						content = protocol.PlainContent(strings.Repeat(plain, row.Repeat))
					} else {
						blocks, _ := content.Blocks()
						for i, block := range blocks {
							if text, ok := block.Text(); ok {
								blocks[i] = protocol.NewTextBlock(strings.Repeat(text.Text, row.Repeat))
							}
						}
						content = protocol.BlockContent(blocks...)
					}
					messages = append(messages, protocol.Message{Role: message.Role, Content: content})
				}
				projected, err = ProjectSessionText(messages, registry, row.MaxChars)
			}
			if err != nil {
				t.Fatal(err)
			}
			wire, err := protocol.PythonJSON(projected, false, true)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(wire))
			if hex.EncodeToString(digest[:]) != row.SHA256 || len(projected.Messages) != row.Count || projected.Coverage != row.Coverage || projected.Omitted != row.Omitted || projected.CompactedHistoryExcluded != row.CompactedResult {
				t.Fatal("source projection differs", wire, row.SHA256)
			}
		})
	}
}

func TestSkillProjectionBoundsAreValidatedAndOutputDetached(t *testing.T) {
	input := []AuthenticatedText{{protocol.RoleUser, "human"}}
	for _, limit := range []int{0, -1} {
		if _, err := ProjectAuthenticatedText(input, nil, ProjectionOptions{MaxChars: limit}); err == nil {
			t.Fatal("invalid budget admitted")
		}
	}
	first, err := ProjectAuthenticatedText(input, nil, DefaultProjectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	first.Messages[0].Content = "changed"
	second, err := ProjectAuthenticatedText(input, nil, DefaultProjectionOptions())
	if err != nil || second.Messages[0].Content != "human" {
		t.Fatal("output aliases input", second, err)
	}
	if _, err := ProjectAuthenticatedText([]AuthenticatedText{{protocol.RoleUser, string([]byte{0xff})}}, nil, DefaultProjectionOptions()); err == nil {
		t.Fatal("invalid UTF8 admitted")
	}
}
