// This small probe is built explicitly by the bounded delivery runner. A changed
// -X main.variant value changes native artifact bytes without changing Go/VCS
// metadata. It is excluded from go test ./... by the testdata directory.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/luoyjx/mini-loop/go/identity"
)

var variant = "first"

func main() {
	remove := flag.Bool("remove-executable", false, "remove this owned probe before first fingerprint read")
	flag.Parse()
	if *remove {
		path, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := os.Remove(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	snapshot, err := identity.Runtime()
	result := struct {
		Variant          string            `json:"variant"`
		Identity         identity.Snapshot `json:"identity"`
		VCSRevision      string            `json:"vcs_revision"`
		VCSModified      bool              `json:"vcs_modified"`
		FingerprintError *string           `json:"fingerprint_error,omitempty"`
	}{Variant: variant, Identity: snapshot}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				result.VCSRevision = setting.Value
			case "vcs.modified":
				result.VCSModified = setting.Value == "true"
			}
		}
	}
	if err != nil {
		message := err.Error()
		result.FingerprintError = &message
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
