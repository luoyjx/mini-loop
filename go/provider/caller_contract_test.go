package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type callerContractFixture struct {
	Cases []callerContractCase `json:"cases"`
}

type callerContractCase struct {
	Mode, SSE, Name string
	Message         json.RawMessage
	Expected        jsonvalue.Value `json:"expected_content"`
}

func TestDeclaredCallerExtensionsMatchSDKDirectAndStream(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-caller.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture callerContractFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 160 {
		t.Fatal("Source inventory changed")
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Mode+"/"+row.Name, func(t *testing.T) {
			var reply protocol.ModelReply
			var err error
			if row.Mode == "stream" {
				reply, err = client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
			} else {
				reply, err = decodeReply(row.Message)
			}
			if err != nil {
				t.Fatal(err)
			}
			content := protocol.BlockContent(reply.Content...)
			assertCallerContractContent(t, "SDK", content, row.Expected)
			clone := content.Clone()
			if content.SameStorage(clone) {
				t.Fatal("clone reused content storage")
			}
			assertCallerContractContent(t, "clone", clone, row.Expected)

			mask := func(value string) string {
				return strings.ReplaceAll(strings.ReplaceAll(value, "secret", "masked"), "parent", "masked-parent")
			}
			masked, err := protocol.MaskedPythonJSON(clone, mask, false, true)
			if err != nil {
				t.Fatal(err)
			}
			gotMasked, err := jsonvalue.Decode(masked)
			if err != nil || !gotMasked.PythonEqual(row.Expected.MapStrings(mask)) {
				t.Fatal("masked SDK observation differs", masked, err)
			}
			assertCallerContractContent(t, "original after mask", content, row.Expected)
			assertCallerContractContent(t, "clone after mask", clone, row.Expected)

			message := protocol.Message{Role: protocol.RoleAssistant, Content: content}
			for _, encoding := range []string{"request", "archive"} {
				var encoded []byte
				if encoding == "request" {
					encoded, err = json.Marshal(message)
				} else {
					encoded, err = message.MarshalArchiveJSON()
				}
				if err != nil {
					t.Fatal(encoding, err)
				}
				restored, err := protocol.DecodeStoredMessage(encoded)
				if err != nil {
					t.Fatal(encoding, err)
				}
				assertCallerContractContent(t, encoding+" roundtrip", restored.Content, row.Expected)
			}

			// Accessor results are detached at every caller-bearing container.
			// Replacing one observed caller must not mutate either transcript copy.
			for _, source := range []protocol.Content{content, clone} {
				blocks, _ := source.Blocks()
				for _, block := range blocks {
					if caller := callerContractBlockCaller(block); caller != nil {
						*caller = protocol.DirectToolCaller()
					}
				}
				blocks[0] = protocol.NewTextBlock("replacement")
			}
			assertCallerContractContent(t, "original after accessor mutation", content, row.Expected)
			assertCallerContractContent(t, "clone after accessor mutation", clone, row.Expected)
		})
	}
}

func assertCallerContractContent(t *testing.T, label string, content protocol.Content, expected jsonvalue.Value) {
	t.Helper()
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(label, err)
	}
	got, err := jsonvalue.Decode(string(encoded))
	if err != nil || !got.PythonEqual(expected) {
		t.Fatal(label, "SDK content projection differs", string(encoded), err)
	}
	blocks, ok := content.Blocks()
	wantBlocks, wantOK := expected.Array()
	if !ok || !wantOK || len(blocks) != len(wantBlocks) {
		t.Fatal(label, "invalid fixture content shape")
	}
	for i, block := range blocks {
		caller := callerContractBlockCaller(block)
		wantCaller, present := wantBlocks[i].Lookup("caller")
		if !present || wantCaller.Kind() == jsonvalue.Null {
			if caller != nil {
				t.Fatal(label, "unexpected typed caller", i)
			}
			continue
		}
		if caller == nil {
			t.Fatal(label, "missing typed caller", i)
		}
		assertCallerContractProjection(t, label, *caller, wantCaller)
		callerJSON, err := json.Marshal(caller)
		if err != nil {
			t.Fatal(label, err)
		}
		var restored protocol.ToolCaller
		if err := json.Unmarshal(callerJSON, &restored); err != nil {
			t.Fatal(label, "caller roundtrip", err)
		}
		assertCallerContractProjection(t, label+" caller roundtrip", restored, wantCaller)
		blockJSON, err := json.Marshal(block)
		if err != nil {
			t.Fatal(label, err)
		}
		var restoredBlock protocol.Block
		if err := json.Unmarshal(blockJSON, &restoredBlock); err != nil {
			t.Fatal(label, "block roundtrip", err)
		}
		restoredCaller := callerContractBlockCaller(restoredBlock)
		if restoredCaller == nil {
			t.Fatal(label, "block roundtrip lost caller")
		}
		assertCallerContractProjection(t, label+" block roundtrip", *restoredCaller, wantCaller)
	}
}

func assertCallerContractProjection(t *testing.T, label string, caller protocol.ToolCaller, expected jsonvalue.Value) {
	t.Helper()
	if err := caller.Validate(); err != nil {
		t.Fatal(label, "SDK observation became invalid", err)
	}
	wantKindValue, _ := expected.Lookup("type")
	wantKind, _ := wantKindValue.Text()
	if string(caller.Kind()) != wantKind {
		t.Fatal(label, "caller discriminator changed", caller.Kind(), wantKind)
	}
	wantID := jsonvalue.NullValue()
	if caller.Kind() != protocol.CallerDirect {
		var present bool
		wantID, present = expected.Lookup("tool_id")
		if !present {
			t.Fatal(label, "Source expected caller omitted normalized tool_id")
		}
	}
	wantText, wantIsText := wantID.Text()
	gotText, gotIsText := caller.ToolIDMetadata().Text()
	wantProjection := ""
	if wantIsText {
		wantProjection = wantText
	}
	if caller.ToolID() != wantProjection || gotIsText != wantIsText || (gotIsText && gotText != wantText) {
		t.Fatal(label, "typed caller ID projection differs")
	}
	if gotID := caller.ToolIDMetadata().Observation(); gotID.Kind() != wantID.Kind() || !gotID.PythonEqual(wantID) {
		t.Fatal(label, "caller ID observation differs")
	}
	encoded, err := json.Marshal(caller)
	if err != nil {
		t.Fatal(label, err)
	}
	got, err := jsonvalue.Decode(string(encoded))
	if err != nil || !got.PythonEqual(expected) {
		t.Fatal(label, "caller observation/extensions changed", string(encoded), err)
	}
}

func callerContractBlockCaller(block protocol.Block) *protocol.ToolCaller {
	if use, ok := block.ToolUse(); ok {
		return use.Caller
	}
	if use, ok := block.ServerToolUse(); ok {
		return use.Caller
	}
	if result, ok := block.WebSearchToolResult(); ok {
		return result.Caller
	}
	if result, ok := block.WebFetchToolResult(); ok {
		return result.Caller
	}
	return nil
}
