package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNonfiniteBashEventArchiveRetainsObservationWithoutAuthority(t *testing.T) {
	for _, number := range []string{"NaN", "Infinity"} {
		raw := []byte(`{"type":"tool_use","seq":1,"ts":1,"session":"session","transcript_epoch":1,"name":"bash","id":"call","input":{"command":"true","keep":` + number + `},"display":{"verb":"Bash","object":"true"}}`)
		record, err := DecodeStoredEvent(raw)
		if err != nil {
			t.Fatal(err)
		}
		if record.Scope.RunContext.authority != AuthorityUntrusted {
			t.Fatal("archive restored live authority")
		}
		output, err := record.MarshalArchiveJSON()
		if err != nil || !strings.Contains(string(output), `"keep":`+number) {
			t.Fatal(string(output), err)
		}
		if _, err = json.Marshal(record); err == nil {
			t.Fatal("legacy observation entered standard finite event JSON")
		}
		reopened, err := DecodeStoredEvent(output)
		if err != nil {
			t.Fatal(err)
		}
		if failure, ok := reopened.Event.toolUse.Input.BashBindingFailure(); !ok || failure.Parameter() != "keep" {
			t.Fatal("binding failure lost after event reconstruction")
		}
	}
}
