package userresources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/luoyjx/mini-loop/go/durable"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/skills"
)

type PublicationCode string

const (
	InvalidOwner      PublicationCode = "invalid_owner"
	SecretDetected    PublicationCode = "secret_detected"
	SecretCheckFailed PublicationCode = "secret_check_failed"
	UserSkillExists   PublicationCode = "user_skill_exists"
	UnsafePath        PublicationCode = "unsafe_path"
	PublishFailed     PublicationCode = "publish_failed"
)

// PublicationError carries only reviewed code/message values, never host faults.
type PublicationError struct {
	code    PublicationCode
	message string
}

func (e *PublicationError) Error() string         { return e.message }
func (e *PublicationError) Code() PublicationCode { return e.code }

type PublicationFailure struct {
	Code  PublicationCode `json:"code"`
	Error string          `json:"error"`
}

func (e *PublicationError) Receipt() PublicationFailure { return PublicationFailure{e.code, e.message} }
func publicationError(code PublicationCode, message string) error {
	return &PublicationError{code, message}
}

type Activation string

const NextSession Activation = "next_session"

type PublicationReceipt struct {
	Name             string     `json:"name"`
	Digest           string     `json:"digest"`
	ContentDigest    string     `json:"content_digest"`
	Activation       Activation `json:"activation"`
	CollisionWarning *string    `json:"collision_warning"`
	Idempotent       bool       `json:"idempotent"`
}

// Publication separates a safe receipt from an immutable internal owner bundle.
type Publication struct {
	receipt   PublicationReceipt
	resources Resources
}

func (p Publication) Resources() Resources { return p.resources }
func (p Publication) Source() memory.Scope { return memory.UserScope }
func (p Publication) Receipt() PublicationReceipt {
	receipt := p.receipt
	if receipt.CollisionWarning != nil {
		warning := *receipt.CollisionWarning
		receipt.CollisionWarning = &warning
	}
	return receipt
}

type registeredSecrets interface{ Names() []secrets.Name }
type secretHealth interface {
	Unresolved() []secrets.Name
	ShortValues() []secrets.Name
}

func screenSkill(masker memory.Masker, fields SkillFields) (err error) {
	if masker == nil {
		return nil
	}
	masking := true
	defer func() {
		if recover() != nil {
			message := "Skill secret screening is unavailable"
			if masking {
				message = "Skill secret screening failed"
			}
			err = publicationError(SecretCheckFailed, message)
		}
	}()
	detected := false
	for _, raw := range []string{fields.Name, fields.Description, fields.Body} {
		if masker.MaskText(raw) != raw {
			detected = true
		}
	}
	if detected {
		return publicationError(SecretDetected, "Skill content contains a registered secret")
	}
	masking = false
	names, ok := masker.(registeredSecrets)
	// A custom masker without a registration surface cannot attest full screening.
	if !ok {
		return publicationError(SecretCheckFailed, "Skill secret screening is unavailable")
	}
	if len(names.Names()) == 0 {
		return nil
	}
	health, ok := masker.(secretHealth)
	if !ok || len(health.Unresolved()) != 0 || len(health.ShortValues()) != 0 {
		return publicationError(SecretCheckFailed, "Skill secret screening is unavailable")
	}
	return nil
}

func publishFault(err error, verification bool) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, errLink) || errors.Is(err, errOutside) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return publicationError(UnsafePath, "User skill path is unsafe")
	}
	message := "User skill could not be published"
	if verification {
		message = "Existing user skill could not be verified"
	}
	return publicationError(PublishFailed, message)
}

func activeSkillMatches(ctx context.Context, catalog *skills.Catalog, target string, canonical CanonicalSkill, bodyDigest string) (bool, error) {
	payload, err := durable.ReadBytesNoFollow(ctx, target, len(canonical.Text()))
	if err != nil {
		if errors.Is(err, durable.ErrTooLarge) || os.IsNotExist(err) {
			return false, nil
		}
		return false, publishFault(err, true)
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != canonical.Digest() {
		return false, nil
	}
	for _, entry := range catalog.Entries() {
		if entry.Name == canonical.Fields().Name {
			return entry.Path == target && entry.Digest == bodyDigest, nil
		}
	}
	return false, nil
}

type createSkillFile func(context.Context, string, string) (durable.FileIdentity, error)

// PublishSkill is an operator operation on a trusted owner. It is not a model
// tool or authenticated route. Callers retain existing live Resources values.
func (r *Resolver) PublishSkill(ctx context.Context, owner OwnerID, fields SkillFields) (Publication, error) {
	return r.publishSkill(ctx, owner, fields, durable.CreateText)
}

func (r *Resolver) publishSkill(ctx context.Context, owner OwnerID, fields SkillFields, create createSkillFile) (Publication, error) {
	key, err := OwnerDirectoryKey(string(owner))
	if err != nil {
		return Publication{}, publicationError(InvalidOwner, "Owner must be a non-empty string")
	}
	canonical, err := NewCanonicalSkill(fields)
	if err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			return Publication{}, publicationError(PublicationCode(validation.Code()), validation.Error())
		}
		return Publication{}, publishFault(err, false)
	}
	if r == nil {
		return Publication{}, publicationError(PublishFailed, "User skill could not be published")
	}
	if err = screenSkill(r.masker, fields); err != nil {
		return Publication{}, err
	}
	if err = r.acquire(ctx); err != nil {
		return Publication{}, publishFault(err, false)
	}
	defer r.release()
	previous, err := r.forOwner(ctx, owner, key)
	if err != nil {
		return Publication{}, publishFault(err, false)
	}
	skillRoot, err := r.directories.directory(ctx, previous.directories.Skills())
	if err != nil {
		return Publication{}, publishFault(err, false)
	}
	current, err := skills.NewCatalog(ctx, skillRoot)
	if err != nil {
		return Publication{}, publishFault(err, false)
	}
	normalized := canonical.Fields()
	target := filepath.Join(skillRoot, normalized.Name, "SKILL.md")
	bodyHash := sha256.Sum256([]byte(normalized.Body))
	receipt := PublicationReceipt{Name: normalized.Name, Digest: canonical.Digest(), ContentDigest: hex.EncodeToString(bodyHash[:]), Activation: NextSession}
	for _, entry := range r.agent.Entries() {
		if entry.Name == normalized.Name {
			warning := "An agent-provided skill has the same name; use an explicit agent: or user: source"
			receipt.CollisionWarning = &warning
			break
		}
	}
	prepare := func(catalog *skills.Catalog, idempotent bool) (Publication, error) {
		layered, err := skills.NewLayeredCatalog(r.agent, catalog)
		if err != nil {
			return Publication{}, publishFault(err, false)
		}
		value := receipt
		value.Idempotent = idempotent
		return Publication{value, Resources{previous.directories, layered, previous.memory}}, nil
	}
	conflict := func() error { return publicationError(UserSkillExists, "A user skill with this name already exists") }
	for _, entry := range current.Entries() {
		if entry.Name != normalized.Name {
			continue
		}
		matches, err := activeSkillMatches(ctx, current, target, canonical, receipt.ContentDigest)
		if err != nil {
			return Publication{}, err
		}
		if !matches {
			return Publication{}, conflict()
		}
		publication, err := prepare(current, true)
		if err != nil {
			return Publication{}, err
		}
		r.resources[owner] = publication.resources
		return publication, nil
	}
	if _, err = r.directories.directory(ctx, filepath.Dir(target)); err != nil {
		return Publication{}, publishFault(err, false)
	}
	prepared, err := current.WithSourceDocument(ctx, target, canonical.Text())
	if err != nil {
		return Publication{}, publishFault(err, false)
	}
	publication, err := prepare(prepared, false)
	if err != nil {
		return Publication{}, err
	}
	_, err = create(ctx, target, canonical.Text())
	if err != nil {
		if !os.IsExist(err) {
			return Publication{}, publishFault(err, false)
		}
		// This call did not commit. A competing publisher may have won the link.
		raced, err := skills.NewCatalog(ctx, skillRoot)
		if err != nil {
			return Publication{}, publishFault(err, false)
		}
		matches, err := activeSkillMatches(ctx, raced, target, canonical, receipt.ContentDigest)
		if err != nil {
			return Publication{}, err
		}
		if !matches {
			return Publication{}, conflict()
		}
		publication, err = prepare(raced, true)
		if err != nil {
			return Publication{}, err
		}
	}
	// No fallible operation or cancellation check may follow a successful link.
	r.resources[owner] = publication.resources
	return publication, nil
}
