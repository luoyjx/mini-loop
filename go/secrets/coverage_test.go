package secrets

import "testing"

func TestCountUnregisteredMatchesCredentialNameSemantics(t *testing.T) {
	environment := Environment{"FIXTURE_API_KEY": "value", "fixture_token": "short", "EMPTY_SECRET": "", "ORDINARY": "value", "AWS_SECRET_ACCESS_KEY": "value"}
	if n := CountUnregistered(environment, nil); n != 3 {
		t.Fatal(n)
	}
	if n := CountUnregistered(environment, []Name{"FIXTURE_API_KEY", "fixture_token", "fixture_token"}); n != 1 {
		t.Fatal(n)
	}
	if n := CountUnregistered(environment, []Name{"FIXTURE_TOKEN"}); n != 3 {
		t.Fatal("registration unexpectedly ignored case", n)
	}
}
