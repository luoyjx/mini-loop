package shell

import (
	"context"
	"fmt"
	"path/filepath"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/spill"
)

// projectBash is the string compatibility surface corresponding to Python
// Toolset.run_bash. ExecuteBashResult remains the unspilled structured surface:
// Python's default Bash adapter currently calls that surface, bypassing this note.
func (executor *Executor) projectBash(ctx context.Context, result Result) string {
	rendered := result.Render()
	if executor.spill == nil || (result.Error != nil && result.Projection == nil) {
		return rendered
	}
	content := result.Stdout + result.Stderr
	if result.Projection != nil {
		content = *result.Projection
	}
	content = pytext.Strip(content)
	if utf8.RuneCountInString(content) <= OutputCap {
		return rendered
	}
	ref, err := preserve(ctx, executor.spill, spill.Request{Namespace: spill.Namespace(filepath.Base(executor.root)), ToolName: "bash", Label: "output", SuggestedName: "bash.txt", Content: content})
	if err != nil {
		return rendered
	}
	return rendered + fmt.Sprintf("\n[full output preserved: %s (%s bytes); %s]", ref.Locator, comma(ref.Bytes), ref.RetrievalHint)
}

// Stores are plugin boundaries. A backend panic must not discard a successful
// command preview, just as an ordinary save error must not replace that preview.
func preserve(ctx context.Context, store spill.Store, request spill.Request) (ref spill.Ref, err error) {
	defer func() {
		if recover() != nil {
			ref = spill.Ref{}
			err = fmt.Errorf("spill backend panic")
		}
	}()
	return store.SaveText(ctx, request)
}
