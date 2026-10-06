package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func (h *runtimeHandler) executeMemory(ctx context.Context, input protocol.ToolInput) (string, error) {
	if h.memory == nil || h.memory.Owner() != memory.OwnerID(h.binding.OwnerID) {
		return "", errors.New("memory tool requires the bound owner's store")
	}
	if input.Name() == protocol.ToolRemember {
		v, _ := input.Remember()
		typ := memory.Project
		if v.Type != nil && v.Type.Valid() {
			typ = memory.Type(*v.Type)
		}
		description := v.Name
		if v.Description != nil && *v.Description != "" {
			description = *v.Description
		}
		return h.memory.Write(ctx, memory.Input{Name: v.Name, Type: typ, Description: description, Body: v.Content, Origin: memory.Explicit})
	}
	v, _ := input.Recall()
	query := ""
	if v.Query != nil {
		query = *v.Query
	}
	hits, err := h.memory.Search(ctx, query, 5)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "(no matching memories)", nil
	}
	blocks := make([]string, 0, len(hits))
	for _, hit := range hits {
		blocks = append(blocks, memoryBlock(hit))
	}
	return strings.Join(blocks, "\n\n"), nil
}

var memoryAttributeEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&#x27;")

func memoryBlock(record memory.Record) string {
	origin := record.Origin
	switch origin {
	case memory.Explicit, memory.AutoExtracted, memory.Consolidated, memory.Imported:
	default:
		origin = memory.Imported
	}
	return "<memory scope=\"user\" origin=\"" + memoryAttributeEscape.Replace(string(origin)) + "\" name=\"" + memoryAttributeEscape.Replace(record.Name) + "\" type=\"" + memoryAttributeEscape.Replace(string(record.Type)) + "\">\n" + record.Body + "\n</memory>"
}
