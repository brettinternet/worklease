package queue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/resource"
)

type Ref struct {
	SourceID string `json:"sourceId"`
	ItemID   string `json:"itemId"`
}

func (r Ref) Key() string    { return r.SourceID + "\x00" + r.ItemID }
func (r Ref) String() string { return r.SourceID + ":" + r.ItemID }
func (r Ref) Less(other Ref) bool {
	if r.SourceID != other.SourceID {
		return r.SourceID < other.SourceID
	}
	return r.ItemID < other.ItemID
}

type Source struct{ ID, Name, Locator, Adapter string }
type Support string

const (
	Supported      Support = "supported"
	Unsupported    Support = "unsupported"
	SupportUnknown Support = "unknown"
)

type Permission string

const (
	Allowed           Permission = "allowed"
	Denied            Permission = "denied"
	PermissionUnknown Permission = "unknown"
)

type Availability string

const (
	Available              Availability = "available"
	Unavailable            Availability = "unavailable"
	AuthenticationRequired Availability = "authentication-required"
)

type Capability struct {
	Support      Support           `json:"support"`
	Permission   Permission        `json:"permission"`
	Availability Availability      `json:"availability"`
	Semantics    map[string]string `json:"semantics,omitempty"`
	Limits       map[string]int    `json:"limits,omitempty"`
	Reason       string            `json:"reason,omitempty"`
}
type CapabilitySet map[string]Capability

type CoverageState string

const (
	CoverageComplete CoverageState = "complete"
	CoveragePartial  CoverageState = "partial"
	CoverageUnknown  CoverageState = "unknown"
)

type TotalAccuracy string

const (
	TotalExact     TotalAccuracy = "exact"
	TotalEstimated TotalAccuracy = "estimated"
	TotalUnknown   TotalAccuracy = "unknown"
)

type Coverage struct {
	State         CoverageState `json:"state"`
	Scope         string        `json:"scope,omitempty"`
	Cursor        string        `json:"cursor,omitempty"`
	Total         int           `json:"total,omitempty"`
	ObservedEdges int           `json:"observedEdges,omitempty"`
	TotalAccuracy TotalAccuracy `json:"totalAccuracy"`
	Reason        string        `json:"reason,omitempty"`
}
type Observation struct {
	Principal               string    `json:"principal,omitempty"`
	AccessScope             string    `json:"-"`
	ConfigurationGeneration string    `json:"configurationGeneration,omitempty"`
	ObservedAt              time.Time `json:"observedAt,omitempty"`
	ProviderVersion         string    `json:"providerVersion,omitempty"`
	Coverage                Coverage  `json:"coverage"`
}
type Summary struct {
	Ref             Ref           `json:"ref"`
	Title           string        `json:"title"`
	RawStatus       string        `json:"rawStatus"`
	State           StateCategory `json:"state"`
	Order           string        `json:"order,omitempty"`
	Priority        int           `json:"priority,omitempty"`
	CanonicalID     string        `json:"canonicalId,omitempty"`
	ProviderReady   *bool         `json:"providerReady,omitempty"`
	AssignedTo      []string      `json:"assignedTo,omitempty"`
	NativeClaim     string        `json:"nativeClaim,omitempty"`
	UpdatedAt       time.Time     `json:"updatedAt,omitempty"`
	Fresh           bool          `json:"fresh"`
	Terminal        bool          `json:"terminal"`
	ProviderBlocked bool          `json:"providerBlocked"`
}
type StateCategory string

const (
	StateOpen       StateCategory = "open"
	StateInProgress StateCategory = "in-progress"
	StateBlocked    StateCategory = "blocked"
	StateComplete   StateCategory = "complete"
	StateUnknown    StateCategory = "unknown"
)

type Item struct {
	Summary
	TerminalKnown     bool             `json:"terminalKnown"`
	Body              string           `json:"body,omitempty"`
	Dependencies      []Ref            `json:"dependencies,omitempty"`
	Relationships     []Relationship   `json:"relationships,omitempty"`
	DependenciesKnown bool             `json:"dependenciesKnown"`
	ReadOutcome       string           `json:"readOutcome,omitempty"`
	ReadPermission    Permission       `json:"readPermission,omitempty"`
	Closure           CoverageState    `json:"closure"`
	Assignment        Assignment       `json:"assignment"`
	Claim             ClaimObservation `json:"claim"`
	Resources         []string         `json:"resources,omitempty"`
	KeyInputs         *resource.Input  `json:"keyInputs,omitempty"`
	Readiness         Readiness        `json:"readiness"`
	Coverage          Coverage         `json:"coverage"`
	Observation       Observation      `json:"observation"`
}
type Assignment struct {
	Assigned bool     `json:"assigned"`
	Owners   []string `json:"owners,omitempty"`
	Known    bool     `json:"known"`
}
type ClaimObservation struct {
	Available     bool      `json:"available"`
	Active        bool      `json:"active"`
	Known         bool      `json:"known"`
	OwnerVerified bool      `json:"ownerVerified"`
	NativeState   string    `json:"nativeState,omitempty"`
	AuthorityID   string    `json:"authorityId,omitempty"`
	State         string    `json:"state,omitempty"`
	AgentID       string    `json:"agentId,omitempty"`
	SessionID     string    `json:"sessionId,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt,omitempty"`
	ObservedAt    time.Time `json:"observedAt,omitempty"`
	Stale         bool      `json:"stale"`
	Reason        string    `json:"reason,omitempty"`
}
type Freshness string

const (
	Fresh            Freshness = "fresh"
	Stale            Freshness = "stale"
	FreshnessUnknown Freshness = "unknown"
)

type ReadinessStatus string

const (
	Ready            ReadinessStatus = "ready"
	Blocked          ReadinessStatus = "blocked"
	ReadinessUnknown ReadinessStatus = "unknown"
)

type Readiness struct {
	Status    ReadinessStatus `json:"status"`
	Reasons   []string        `json:"reasons,omitempty"`
	Freshness Freshness       `json:"freshness"`
}
type RelationshipType string

const (
	HardPrerequisite        RelationshipType = "hard-prerequisite"
	CrossSourcePrerequisite RelationshipType = "cross-source-prerequisite"
	ParentChild             RelationshipType = "parent-child"
	Related                 RelationshipType = "related"
)

type Direction string

const (
	DependentToPrerequisite Direction = "dependent-to-prerequisite"
	ParentToChild           Direction = "parent-to-child"
	NonBlockingDirection    Direction = "non-blocking"
	UnknownDirection        Direction = "unknown"
)

type Relationship struct {
	Type           RelationshipType `json:"type"`
	Direction      Direction        `json:"direction"`
	From           Ref              `json:"from"`
	To             Ref              `json:"to"`
	Provenance     string           `json:"provenance"`
	Condition      string           `json:"condition,omitempty"`
	RawOutcome     string           `json:"rawOutcome,omitempty"`
	Interpretation string           `json:"interpretation,omitempty"`
	Fresh          bool             `json:"fresh"`
	Support        Support          `json:"support,omitempty"`
}
type DependencyPage struct {
	Edges        []Relationship
	NextCursor   string
	Completeness CoverageState
	Observation  Observation
}
type ItemOutcome struct {
	Ref         Ref
	Item        *Item
	Kind        string
	Err         error
	Observation Observation
}
type SummaryPage struct {
	Items       []Summary
	NextCursor  string
	Coverage    Coverage
	Observation Observation
}
type Query struct {
	Filters Filters
	Fields  []string
	Budget  int
}
type Filters struct {
	States    []StateCategory
	SourceIDs []string
	Text      string
}
type View struct {
	SourceOrder []string
	Filters     Filters
}
type Action string

const (
	ActionStart          Action = "start"
	ActionClaim          Action = "claim"
	ActionLaunch         Action = "launch"
	ActionResume         Action = "resume"
	ActionReportBlocked  Action = "report-blocked"
	ActionRecordProgress Action = "record-progress"
	ActionComplete       Action = "complete"
)

type Eligibility struct {
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons,omitempty"`
	Requires []string `json:"requires,omitempty"`
	Outcome  string   `json:"outcome,omitempty"`
}

func canonical(item Item) string {
	if item.CanonicalID != "" {
		return item.CanonicalID
	}
	return item.Ref.Key()
}
func cloneItem(i Item) Item {
	i.AssignedTo = append([]string(nil), i.AssignedTo...)
	if i.ProviderReady != nil {
		value := *i.ProviderReady
		i.ProviderReady = &value
	}
	i.Dependencies = append([]Ref(nil), i.Dependencies...)
	if i.Relationships != nil {
		i.Relationships = append(make([]Relationship, 0, len(i.Relationships)), i.Relationships...)
	}
	i.Readiness.Reasons = append([]string(nil), i.Readiness.Reasons...)
	i.Assignment.Owners = append([]string(nil), i.Assignment.Owners...)
	i.Resources = append([]string(nil), i.Resources...)
	if i.KeyInputs != nil {
		key := *i.KeyInputs
		i.KeyInputs = &key
	}
	return i
}
func cloneItems(in map[string]Item) map[string]Item {
	out := make(map[string]Item, len(in))
	for k, v := range in {
		out[k] = cloneItem(v)
	}
	return out
}
func validRef(r Ref) bool {
	return strings.TrimSpace(r.SourceID) != "" && strings.TrimSpace(r.ItemID) != ""
}

type Adapter interface {
	Resolve(context.Context, map[string]string) (Source, error)
	Capabilities(context.Context, Source, string, *Ref) (CapabilitySet, error)
	List(context.Context, Source, Query, string) (SummaryPage, error)
	ReadItems(context.Context, Source, []Ref, []string, int) []ItemOutcome
	ReadDependencies(context.Context, Source, Ref, string, int) (DependencyPage, error)
}
type Registry struct{ adapters map[string]Adapter }

func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{"backlog-md": NewBacklogAdapter(), "github": NewGitHubAdapter()}}
}
func (r *Registry) Register(name string, a Adapter) error {
	if strings.TrimSpace(name) == "" || a == nil {
		return fmt.Errorf("adapter name and implementation are required")
	}
	if _, ok := r.adapters[name]; ok {
		return fmt.Errorf("adapter %q already registered", name)
	}
	r.adapters[name] = a
	return nil
}
func (r *Registry) Get(name string) (Adapter, bool) { a, ok := r.adapters[name]; return a, ok }
