package domain

import "time"

// ExtractionRuleStatus indicates the lifecycle state of an ExtractionRule.
type ExtractionRuleStatus string

const (
	ExtractionRuleStatusActive     ExtractionRuleStatus = "active"
	ExtractionRuleStatusSuperseded ExtractionRuleStatus = "superseded"
	ExtractionRuleStatusBroken     ExtractionRuleStatus = "broken"
)

// ExtractionRuleKind identifies the domain that the TargetID belongs to.
// For FX monitoring the kind is "rate". Future kinds (weather, commodity, …)
// reuse the same table without any schema change.
type ExtractionRuleKind string

const (
	ExtractionRuleKindRate ExtractionRuleKind = "rate"
)

// ExtractionRule is a target-kind-agnostic record that pairs a URL with an
// extraction pattern and tracks provenance.  For kind="rate", TargetID equals
// rate_sources.name.  For future kinds the consumer interprets TargetID however
// it likes — the rules table stores it as an opaque string.
//
// Label anchors the rule within a target (e.g. "EUR / KZT" for rate sources
// that expose multiple pairs at the same URL).  Single-datum targets use "".
type ExtractionRule struct {
	ID             string
	TargetKind     ExtractionRuleKind
	TargetID       string
	Label          string // pair name or "" for single-datum targets
	SourceURL      string
	Method         Method
	Pattern        string
	ProviderTag    string
	ContextHash    string
	Status         ExtractionRuleStatus
	GeneratedAt    time.Time
	LastVerifiedAt *time.Time
	Notes          string
}
