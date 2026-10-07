package jsonvalue

import (
	"errors"
	"strings"
	"testing"
)

func TestUTF8CanonicalBytesPreserveSourceIdentity(t *testing.T) {
	// Golden bytes from Python json.dumps(ensure_ascii=False, sort_keys=True,
	// separators=(",", ":"), allow_nan=False), the workflow canonical profile.
	v, err := Decode(`{"中":[1.0,"<>&\u2028😀",null],"a":-0.0}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Sorted().MarshalUTF8()
	want := "{\"a\":-0.0,\"中\":[1.0,\"<>&\u2028😀\",null]}"
	if err != nil || string(got) != want {
		t.Fatalf("%s, want %s: %v", got, want, err)
	}
	archive, err := v.MarshalJSON()
	if err != nil || !strings.Contains(string(archive), `\u4e2d`) {
		t.Fatal("archive profile changed", string(archive), err)
	}
	for _, raw := range []string{`NaN`, `{"x":Infinity}`, `["\ud800"]`, `{"\ud800":1}`} {
		v, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.MarshalUTF8(); !errors.Is(err, ErrNonfinite) && !errors.Is(err, ErrSurrogate) {
			t.Fatalf("accepted invalid UTF-8 profile: %s: %v", raw, err)
		}
	}
}
