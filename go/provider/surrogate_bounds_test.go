package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestSDKUnicodeObservationRetainsNormalizedUTF8Budget(t *testing.T) {
	// Escaping these ordinary Chinese code points would exceed the byte limit,
	// while their real UTF-8 observation remains below it.
	text := strings.Repeat("中", (protocol.MaxWireBytes-256)/3)
	if err := checkSDKBlockSize(protocol.NewTextBlock(text)); err != nil {
		t.Fatal("ASCII escape inflation changed the ordinary UTF-8 limit", err)
	}
	high := string([]byte{0xed, 0xa0, 0x80})
	if err := checkSDKBlockSize(protocol.NewTextBlock(text + high)); err != nil {
		t.Fatal("surrogatepass text-byte budget changed", err)
	}
	if err := checkSDKBlockSize(protocol.NewTextBlock(strings.Repeat("中", protocol.MaxWireBytes/3+1) + high)); err == nil {
		t.Fatal("surrogate observation bypassed the existing normalized byte limit")
	}
}

func TestSDKUnicodeBridgeRetainsActualDecoderUTF8Budget(t *testing.T) {
	text := strings.Repeat("中", (protocol.MaxWireBytes-512)/3)
	// Both fixtures are legal UTF-8 JSON wire documents with literal Unicode and
	// one escaped lone surrogate. Their actual decoder input is below the cap.
	cases := []struct{ name, raw string }{
		{"input", `{"type":"tool_use","id":"client","name":"bash","input":{"command":"true","future":"` + text + `\ud800"}}`},
		{"caller", `{"type":"tool_use","id":"client","name":"bash","input":{"command":"true"},"caller":{"type":"code_execution_20260120","tool_id":"` + text + `\ud800"}}`},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			if len(row.raw) >= protocol.MaxWireBytes || !json.Valid([]byte(row.raw)) {
				t.Fatal("invalid bounded Unicode wire recipe")
			}
			block, err := decodeBlock([]byte(row.raw))
			if err != nil {
				t.Fatal("surrogate observation inflated legitimate UTF-8 across the real decoder bound", err)
			}
			ascii, err := block.MarshalObservationASCIIJSON()
			if err != nil || !strings.Contains(string(ascii), `\ud800`) {
				t.Fatal("bounded decode lost original surrogate", err)
			}
			if _, err := json.Marshal(block); err == nil {
				t.Fatal("bounded observation widened HTTP encoding")
			}
		})
	}
}
