// Package memory implements explicit Markdown memory stores. Runtime extraction
// and tool activation are separate composition steps.
package memory

const MaxSlug = 80
const MaxBody = 32000
const MaxIndex = 8000
const MaxProblems = 50

type OwnerID string
type Type string

const (
	User      Type = "user"
	Feedback  Type = "feedback"
	Project   Type = "project"
	Reference Type = "reference"
)

type Origin string

const (
	Explicit      Origin = "explicit"
	AutoExtracted Origin = "auto_extracted"
	Consolidated  Origin = "consolidated"
	Imported      Origin = "imported"
)

type Scope string

const UserScope Scope = "user"

type Input struct {
	Name              string
	Type              Type
	Description, Body string
	Origin            Origin
}
type Record struct {
	File        string `json:"file"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	OwnerKey    string `json:"owner_key"`
	Scope       Scope  `json:"scope"`
	Origin      Origin `json:"origin"`
	Description string `json:"description"`
	Type        Type   `json:"type"`
	Body        string `json:"body"`
}
type Masker interface{ MaskText(string) string }
type Problem struct {
	Message string
	Count   int
}
