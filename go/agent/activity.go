package agent

import (
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"strings"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type ToolDisplay struct{ Verb, Object string }

func oneLine(value string, cap int) string {
	text := strings.Join(strings.FieldsFunc(value, pytext.IsSpace), " ")
	runes := []rune(text)
	if len(runes) > cap {
		return string(runes[:cap-1]) + "…"
	}
	return text
}

func ActivityTitle(text string) (string, bool) {
	lines := strings.FieldsFunc(pytext.Strip(text), func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == 0x1c || r == 0x1d || r == 0x1e || r == 0x85 || r == 0x2028 || r == 0x2029
	})
	if len(lines) == 0 {
		return "", false
	}
	line := pytext.Strip(strings.TrimLeft(pytext.Strip(lines[0]), "#*->• "))
	cut := len(line)
	for _, stop := range []string{". ", "。", "! ", "！", "? ", "？", "; ", "；"} {
		if i := strings.Index(line, stop); i >= 0 {
			_, size := firstRune(stop)
			if i+size < cut {
				cut = i + size
			}
		}
	}
	title := oneLine(pytext.Strip(line[:cut]), 80)
	return title, title != ""
}
func firstRune(text string) (rune, int) {
	for _, r := range text {
		return r, len(string(r))
	}
	return 0, 0
}

func ToolLabel(input protocol.ToolInput) ToolDisplay {
	switch input.Name() {
	case protocol.ToolReadFile:
		v, _ := input.ReadFile()
		return ToolDisplay{"read", oneLine(v.Path, 60)}
	case protocol.ToolWriteFile:
		v, _ := input.WriteFile()
		return ToolDisplay{"write", oneLine(v.Path, 60)}
	case protocol.ToolEditFile:
		v, _ := input.EditFile()
		return ToolDisplay{"edit", oneLine(v.Path, 60)}
	case protocol.ToolGlob:
		v, _ := input.Glob()
		return ToolDisplay{"search", oneLine(v.Pattern, 60)}
	case protocol.ToolBash:
		v, _ := input.Bash()
		preview := oneLine(v.Command, 60)
		for _, marker := range []string{"|", "$(", "`", ">", "<", ";", "&&", "||", "\n"} {
			if strings.Contains(v.Command, marker) {
				return ToolDisplay{"run", preview}
			}
		}
		tokens := strings.Fields(v.Command)
		if len(tokens) == 0 {
			return ToolDisplay{"run", preview}
		}
		verb := ""
		switch tokens[0] {
		case "rg", "grep", "egrep", "fgrep":
			verb = "search"
		case "ls", "tree":
			verb = "list"
		case "cat", "head", "tail", "wc":
			verb = "read"
		default:
			return ToolDisplay{"run", preview}
		}
		rest := oneLine(strings.Join(tokens[1:], " "), 60)
		if rest == "" {
			rest = "."
		}
		return ToolDisplay{verb, rest}
	default:
		return ToolDisplay{"call", string(input.Name())}
	}
}
