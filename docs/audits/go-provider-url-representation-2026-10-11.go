package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

type observation struct {
	Mode        string `json:"mode"`
	URL         string `json:"url"`
	EscapedPath string `json:"escaped_path"`
	RequestURI  string `json:"request_uri"`
	DirectLine  string `json:"direct_line"`
	ProxyLine   string `json:"proxy_line"`
}

func main() {
	rows := []observation{}
	for _, mode := range []string{"normal", "origin-opaque", "absolute-opaque"} {
		r, err := http.NewRequest("POST", "http://fixture.invalid/ascii-|-tail/v1/messages", nil)
		if err != nil {
			panic(err)
		}
		switch mode {
		case "origin-opaque":
			r.URL.Opaque = "/ascii-|-tail/v1/messages"
		case "absolute-opaque":
			r.URL.Opaque = "//fixture.invalid/ascii-|-tail/v1/messages"
		}
		var direct, proxy bytes.Buffer
		if err = r.Write(&direct); err != nil {
			panic(err)
		}
		if err = r.WriteProxy(&proxy); err != nil {
			panic(err)
		}
		rows = append(rows, observation{mode, r.URL.String(), r.URL.EscapedPath(), r.URL.RequestURI(), strings.Split(direct.String(), "\r\n")[0], strings.Split(proxy.String(), "\r\n")[0]})
	}
	if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
		panic(err)
	}
}
