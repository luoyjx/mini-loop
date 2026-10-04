package httpapi

import (
	"embed"
	"net/http"
	"strings"
)

// Browser sources are copied by python/tools/export_go_webui.py. No host static
// directory is mounted: both documents are assembled from immutable embed inputs.
//
//go:embed uiassets/console.html uiassets/index.html uiassets/app.css uiassets/app.js
var browserAssets embed.FS

func browserDocument(name string) string {
	data, err := browserAssets.ReadFile("uiassets/" + name)
	if err != nil {
		panic("missing embedded browser source: " + name)
	}
	return string(data)
}

var consolePage = browserDocument("console.html")
var webUIPage = strings.Replace(strings.Replace(browserDocument("index.html"),
	"/*CSS*/", browserDocument("app.css"), 1), "/*JS*/", browserDocument("app.js"), 1)

func (s *Server) console(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(consolePage))
}

func (s *Server) webUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(webUIPage))
}
