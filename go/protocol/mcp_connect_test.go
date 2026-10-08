package protocol

import "testing"

func TestConnectMCPInputRoundTripAndMask(t *testing.T) {
	for _, raw := range []string{`{"name":"docs"}`, `{"name":""}`} {
		input, err := DecodeToolInput(ToolConnectMCP, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := input.MarshalJSON()
		if err != nil || string(wire) != raw {
			t.Fatalf("round trip: %s %v", wire, err)
		}
		canonical, err := input.CanonicalJSON()
		if err != nil || canonical != raw {
			t.Fatal(canonical, err)
		}
		masked := MapToolInputStrings(input, func(string) string { return "hidden" })
		if value, _ := masked.ConnectMCP(); value.Name != "hidden" {
			t.Fatal("connection alias not masked")
		}
		original, _ := input.MarshalJSON()
		if string(original) != raw {
			t.Fatal("mask changed live alias")
		}
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `{"name":null}`, `{"name":1}`} {
		if _, err := DecodeToolInput(ToolConnectMCP, []byte(raw)); err == nil {
			t.Fatalf("accepted invalid input %s", raw)
		}
	}
}
