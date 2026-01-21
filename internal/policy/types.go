package policy

// Policy represents a BuildKit source policy
// See: https://github.com/moby/buildkit/blob/master/sourcepolicy/pb/policy.proto
type Policy struct {
	Version int64  `json:"version"`
	Rules   []Rule `json:"rules"`
}

// Rule defines the action(s) to take when a source is matched
type Rule struct {
	Action   PolicyAction `json:"action"`
	Selector Selector     `json:"selector"`
	Updates  *Update      `json:"updates,omitempty"`
}

// Selector identifies a source to match a policy to
type Selector struct {
	Identifier  string           `json:"identifier"`
	MatchType   MatchType        `json:"matchType,omitempty"`
	Constraints []AttrConstraint `json:"constraints,omitempty"`
}

// Update contains updates to the matched build step after rule is applied
type Update struct {
	Identifier string            `json:"identifier,omitempty"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

// AttrConstraint defines a constraint on a source attribute
type AttrConstraint struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Condition AttrMatch `json:"condition,omitempty"`
}

// PolicyAction defines the action to take when a source is matched
type PolicyAction string

const (
	PolicyActionAllow   PolicyAction = "ALLOW"
	PolicyActionDeny    PolicyAction = "DENY"
	PolicyActionConvert PolicyAction = "CONVERT"
)

// MatchType is used to determine how a rule source is matched
type MatchType string

const (
	MatchTypeWildcard MatchType = "WILDCARD"
	MatchTypeExact    MatchType = "EXACT"
	MatchTypeRegex    MatchType = "REGEX"
)

// AttrMatch defines the condition to match a source attribute
type AttrMatch string

const (
	AttrMatchEqual    AttrMatch = "EQUAL"
	AttrMatchNotEqual AttrMatch = "NOTEQUAL"
	AttrMatchMatches  AttrMatch = "MATCHES"
)

// NewPolicy creates a new policy with the default version
func NewPolicy() *Policy {
	return &Policy{
		Version: 1,
		Rules:   []Rule{},
	}
}

// AddPinRule adds a rule that pins an image reference to a specific digest
func (p *Policy) AddPinRule(originalRef, pinnedRef string) {
	rule := Rule{
		Action: PolicyActionConvert,
		Selector: Selector{
			Identifier: "docker-image://" + originalRef,
			MatchType:  MatchTypeExact,
		},
		Updates: &Update{
			Identifier: "docker-image://" + pinnedRef,
		},
	}
	p.Rules = append(p.Rules, rule)
}
