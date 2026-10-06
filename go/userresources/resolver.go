package userresources

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/skills"
)

// Resources pins one owner's catalogue and mutable memory service. Its private
// fields cannot be rebound by a live caller. It is not a model/HTTP receipt.
type Resources struct {
	directories DirectoryBinding
	skills      *skills.LayeredCatalog
	memory      *memory.ScopedStore
}

func (r Resources) Owner() OwnerID                 { return r.directories.Owner() }
func (r Resources) Directories() DirectoryBinding  { return r.directories }
func (r Resources) Scope() memory.Scope            { return memory.UserScope }
func (r Resources) Skills() *skills.LayeredCatalog { return r.skills }
func (r Resources) Memory() *memory.ScopedStore    { return r.memory }

// Resolver serializes complete owner snapshot construction. Publication and
// managed session activation are distinct operations, not implicit refreshes.
type Resolver struct {
	directories *DirectoryResolver
	agent       *skills.Catalog
	masker      memory.Masker
	permit      chan struct{}
	resources   map[OwnerID]Resources
	ordered     []OwnerID
}

func NewResolver(ctx context.Context, root string, agent *skills.Catalog, masker memory.Masker) (*Resolver, error) {
	if agent == nil {
		return nil, errors.New("agent skill catalogue is required")
	}
	directories, err := NewDirectoryResolver(ctx, root)
	if err != nil {
		return nil, err
	}
	resolver := &Resolver{directories: directories, agent: agent, masker: masker, permit: make(chan struct{}, 1), resources: make(map[OwnerID]Resources)}
	resolver.permit <- struct{}{}
	return resolver, nil
}
func (r *Resolver) acquire(ctx context.Context) error {
	if r == nil || r.permit == nil {
		return errors.New("user resource resolver is unavailable")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.permit:
	}
	if err := ctx.Err(); err != nil {
		r.release()
		return err
	}
	return nil
}
func (r *Resolver) release() { r.permit <- struct{}{} }
func (r *Resolver) ForOwner(ctx context.Context, owner OwnerID) (Resources, error) {
	key, err := OwnerDirectoryKey(string(owner))
	if err != nil {
		return Resources{}, err
	}
	if err := r.acquire(ctx); err != nil {
		return Resources{}, err
	}
	defer r.release()
	if resources, ok := r.resources[owner]; ok {
		return resources, nil
	}
	// Recheck all paths until a complete Resources value is committed. A prior
	// cancelled catalogue construction must not publish a directory-only cache.
	root, err := r.directories.directory(ctx, filepath.Join(r.directories.root, string(key)))
	if err != nil {
		return Resources{}, err
	}
	skillRoot, err := r.directories.directory(ctx, filepath.Join(root, "skills"))
	if err != nil {
		return Resources{}, err
	}
	memoryRoot, err := r.directories.directory(ctx, filepath.Join(root, "memory"))
	if err != nil {
		return Resources{}, err
	}
	user, err := skills.NewCatalog(ctx, skillRoot)
	if err != nil {
		return Resources{}, err
	}
	layered, err := skills.NewLayeredCatalog(r.agent, user)
	if err != nil {
		return Resources{}, err
	}
	store, err := memory.NewStore(ctx, memoryRoot, r.masker)
	if err != nil {
		return Resources{}, err
	}
	bound, err := memory.Bind(store, memory.OwnerID(owner))
	if err != nil {
		return Resources{}, err
	}
	if err := ctx.Err(); err != nil {
		return Resources{}, err
	}
	resources := Resources{DirectoryBinding{owner, key, root, skillRoot, memoryRoot}, layered, bound}
	r.resources[owner] = resources
	r.ordered = append(r.ordered, owner)
	return resources, nil
}

// Problems is a fresh bounded operator projection. Deployment diagnostics are
// audited through the shared agent catalogue and are not repeated per owner.
func (r *Resolver) Problems(ctx context.Context) ([]skills.Problem, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	snapshots := make([]Resources, 0, len(r.ordered))
	for _, owner := range r.ordered {
		snapshots = append(snapshots, r.resources[owner])
	}
	r.release()
	problems := []skills.Problem{}
	for _, resource := range snapshots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, problem := range resource.skills.Problems() {
			if strings.HasPrefix(problem.Message, "agent:") {
				continue
			}
			message := problem.Message
			if problem.Count > 1 {
				message += fmt.Sprintf(" (x%d)", problem.Count)
			}
			message = string(resource.directories.Key()) + ": " + message
			if len(problems) == skills.MaxProblems {
				problems = problems[1:]
			}
			problems = append(problems, skills.Problem{Kind: problem.Kind, Message: message, Count: 1})
		}
	}
	return problems, nil
}
