package httpapi

import (
	"net/http"
	"strings"
)

type routeVerb string

const (
	routeGet    routeVerb = http.MethodGet
	routePost   routeVerb = http.MethodPost
	routeDelete routeVerb = http.MethodDelete
)

type routeMethod struct {
	verb    routeVerb
	handler http.HandlerFunc
}

type routeSegmentKind uint8

const (
	routeLiteral routeSegmentKind = iota
	routeParameter
)

type routeSegment struct {
	kind  routeSegmentKind
	value string
}

type registeredRoute struct {
	segments []routeSegment
	methods  []routeMethod
}

// Registration freezes ordered methods. Source advertises the first matching
// route's methods on 405, while still accepting later registered methods.
func (s *Server) register(path string, methods ...routeMethod) {
	if len(methods) == 0 {
		panic("route requires a method")
	}
	parts := strings.Split(path, "/")
	segments := make([]routeSegment, len(parts))
	for i, part := range parts {
		segments[i] = routeSegment{routeLiteral, part}
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			segments[i] = routeSegment{routeParameter, part[1 : len(part)-1]}
		}
	}
	s.registered = append(s.registered, registeredRoute{segments, append([]routeMethod(nil), methods...)})
}

func (route registeredRoute) matches(parts []string) bool {
	if len(route.segments) != len(parts) {
		return false
	}
	for i, segment := range route.segments {
		switch segment.kind {
		case routeLiteral:
			if segment.value != parts[i] {
				return false
			}
		case routeParameter:
			if parts[i] == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Operate on the fully decoded path, like ASGI. Do not clean dot/empty segments
// or match an escaped slash as part of a single parameter (ServeMux defaults).
func (s *Server) serveRoutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	for _, route := range s.registered {
		if !route.matches(parts) {
			continue
		}
		for _, method := range route.methods {
			if string(method.verb) != r.Method {
				continue
			}
			for i, segment := range route.segments {
				if segment.kind == routeParameter {
					r.SetPathValue(segment.value, parts[i])
				}
			}
			method.handler(w, r)
			return
		}
		w.Header().Set("Allow", string(route.methods[0].verb))
		writeJSON(s, w, http.StatusMethodNotAllowed, ErrorResponse{"Method Not Allowed"})
		return
	}
	if r.URL.Path != "/" {
		candidate := r.URL.Path + "/"
		if strings.HasSuffix(r.URL.Path, "/") {
			candidate = strings.TrimRight(r.URL.Path, "/")
		}
		for _, route := range s.registered {
			if route.matches(strings.Split(candidate, "/")) {
				redirectRoute(w, r, candidate)
				return
			}
		}
	}
	writeJSON(s, w, http.StatusNotFound, ErrorResponse{"Not Found"})
}

func redirectRoute(w http.ResponseWriter, r *http.Request, path string) {
	target := *r.URL
	target.Path, target.RawPath, target.Host = path, "", r.Host
	if target.Scheme == "" {
		target.Scheme = "http"
		if r.TLS != nil {
			target.Scheme = "https"
		}
	}
	w.Header().Set("Location", target.String())
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusTemporaryRedirect)
}
