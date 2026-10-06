package userresources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type sourcePreviewModel struct {
	responses  []string
	registry   *secrets.Registry
	shortAfter bool
	calls      []string
}

type sourcePreviewMessage struct {
	Content string        `json:"content"`
	Role    protocol.Role `json:"role"`
}

func (model *sourcePreviewModel) CompletePersonalSkillPreview(_ context.Context, request SkillPreviewRequest) (string, error) {
	wire, err := protocol.PythonJSON(struct {
		Immutable bool                   `json:"immutable_messages"`
		MaxTokens int                    `json:"max_tokens"`
		Messages  []sourcePreviewMessage `json:"messages"`
		Purpose   string                 `json:"purpose"`
		System    string                 `json:"system"`
		Tools     []protocol.ToolSchema  `json:"tools"`
	}{true, request.MaxTokens(), []sourcePreviewMessage{{Role: protocol.RoleUser, Content: request.Prompt()}}, "personal_skill_preview", request.System(), []protocol.ToolSchema{}}, false, true)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(wire))
	model.calls = append(model.calls, hex.EncodeToString(digest[:]))
	if len(model.calls) > len(model.responses) {
		return "", errors.New("unexpected model call")
	}
	response := model.responses[len(model.calls)-1]
	if response == "provider-error" {
		return "", errors.New("private-provider-error")
	}
	if response == "cancelled" {
		return "", context.Canceled
	}
	if model.shortAfter {
		model.registry.RegisterValue("new-short", "tiny")
	}
	return response, nil
}

type previewFunction func(context.Context, SkillPreviewRequest) (string, error)

func (model previewFunction) CompletePersonalSkillPreview(ctx context.Context, request SkillPreviewRequest) (string, error) {
	return model(ctx, request)
}

func TestSkillPreviewNativeFaultsAndCancellationCannotRetainDraft(t *testing.T) {
	history := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("human")}}
	input := SkillPreviewInput{Owner: "owner", Session: "session", Name: "recipe", History: history}
	panicModel := previewFunction(func(context.Context, SkillPreviewRequest) (string, error) { panic("private model panic") })
	for _, cfg := range []SkillPreviewConfig{{}, {Model: panicModel}, {Model: panicModel, Secrets: capturePanicMasker{}}} {
		previewer, err := NewSkillPreviewer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = previewer.Preview(context.Background(), input)
		var failure *DraftError
		want := DraftProviderFailure
		if cfg.Secrets != nil {
			want = DraftScreeningUnavailable
		}
		if !errors.As(err, &failure) || failure.Code() != want || len(previewer.drafts.drafts) != 0 {
			t.Fatal("fault escaped or retained draft", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := previewFunction(func(context.Context, SkillPreviewRequest) (string, error) {
		cancel()
		return `{"schema":"mini-loop.personal-skill-draft/v1","decision":"create","description":"recipe","body":"body","evidence_indexes":[0]}`, nil
	})
	previewer, err := NewSkillPreviewer(SkillPreviewConfig{Model: model})
	if err != nil {
		t.Fatal(err)
	}
	_, err = previewer.Preview(ctx, input)
	if !errors.Is(err, context.Canceled) || len(previewer.drafts.drafts) != 0 {
		t.Fatal("cancelled provider landing retained draft", err)
	}
}

func TestSkillPreviewMatchesActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-skill-preview.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name          string
			RequestedName string `json:"requested_name"`
			Owner         OwnerID
			Session       DraftSessionID
			Focus         string
			Ledger        bool
			Turns         int
			User, Final   string
			History       []protocol.Message
			Values        []string
			Minimum       int
			Unresolved    bool
			Responses     []string
			ShortAfter    bool `json:"short_after"`
			Expected      struct {
				Error   DraftCode
				Status  int
				Preview *DraftPreview
				Calls   []string
			}
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 29 {
		t.Fatal("incomplete source preview corpus", err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			registry := secrets.New(secrets.Config{MinLength: &row.Minimum})
			for i, value := range row.Values {
				registry.RegisterValue(secrets.Name(string(rune('0'+i))), value)
			}
			if row.Unresolved {
				registry.RegisterLookup("missing", func() (string, error) { return "", errors.New("private missing") })
			}
			model := &sourcePreviewModel{responses: row.Responses, registry: registry, shortAfter: row.ShortAfter}
			cfg := DefaultDraftStoreConfig()
			cfg.Clock = func() time.Time { return time.Unix(100, 0) }
			store, err := NewDraftStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			previewer, err := NewSkillPreviewer(SkillPreviewConfig{Model: model, Secrets: registry, Drafts: store})
			if err != nil {
				t.Fatal(err)
			}
			input := SkillPreviewInput{Owner: row.Owner, Session: row.Session, Name: row.RequestedName, Focus: row.Focus, History: row.History}
			if row.Ledger {
				input.Ledger = &CaptureLedger{}
				for i := 0; i < row.Turns; i++ {
					input.Ledger.Record(row.User, row.Final, row.History, registry)
				}
			}
			draft, err := previewer.Preview(context.Background(), input)
			if len(model.calls) != len(row.Expected.Calls) || (len(model.calls) > 0 && !reflect.DeepEqual(model.calls, row.Expected.Calls)) {
				t.Fatal("source requests differ", model.calls, row.Expected.Calls)
			}
			if row.Expected.Error == "cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation mapped to provider failure", err)
				}
			} else if row.Expected.Error != "" {
				var failure *DraftError
				if !errors.As(err, &failure) || failure.Code() != row.Expected.Error || failure.StatusCode() != row.Expected.Status {
					t.Fatal("source refusal differs", err, row.Expected.Error, row.Expected.Status)
				}
			} else {
				if err != nil || row.Expected.Preview == nil {
					t.Fatal(err)
				}
				got, want := draft.Preview(), *row.Expected.Preview
				if got.SkillFields != want.SkillFields || got.Digest != want.Digest || got.Coverage != want.Coverage || got.Omitted != want.Omitted || got.CompactedHistoryExcluded != want.CompactedHistoryExcluded || !reflect.DeepEqual(got.EvidenceIndexes, want.EvidenceIndexes) {
					t.Fatal("source preview differs", got, want)
				}
				retained, err := store.Peek(DraftQuery{ID: got.ID, Owner: row.Owner, Session: row.Session, Digest: &got.Digest})
				if err != nil || retained.Preview().Digest != got.Digest {
					t.Fatal("accepted draft not bound/retained", err)
				}
				if got.CreatedAt != 100 || got.ExpiresAt != 100+cfg.TTL.Seconds() {
					t.Fatal("native retained lifetime differs", got)
				}
			}
			if row.Expected.Error != "" && len(store.drafts) != 0 {
				t.Fatal("failed preview retained a draft")
			}
		})
	}
}
