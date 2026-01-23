// Package policy provides helper functions for creating BuildKit source policies.
// It wraps the official github.com/moby/buildkit/sourcepolicy/pb types.
package policy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/sourcepolicy"
	spb "github.com/moby/buildkit/sourcepolicy/pb"
)

// Re-export types from buildkit sourcepolicy/pb for convenience
type (
	Policy         = spb.Policy
	Rule           = spb.Rule
	Selector       = spb.Selector
	Update         = spb.Update
	AttrConstraint = spb.AttrConstraint
	PolicyAction   = spb.PolicyAction
	MatchType      = spb.MatchType
	AttrMatch      = spb.AttrMatch
)

// Re-export constants
const (
	PolicyActionAllow   = spb.PolicyAction_ALLOW
	PolicyActionDeny    = spb.PolicyAction_DENY
	PolicyActionConvert = spb.PolicyAction_CONVERT

	MatchTypeWildcard = spb.MatchType_WILDCARD
	MatchTypeExact    = spb.MatchType_EXACT
	MatchTypeRegex    = spb.MatchType_REGEX

	AttrMatchEqual    = spb.AttrMatch_EQUAL
	AttrMatchNotEqual = spb.AttrMatch_NOTEQUAL
	AttrMatchMatches  = spb.AttrMatch_MATCHES
)

// NewPolicy creates a new policy with the default version
func NewPolicy() *Policy {
	return &Policy{
		Version: 1,
		Rules:   []*Rule{},
	}
}

// AddPinRule adds a rule that pins an image reference to a specific digest
func AddPinRule(p *Policy, originalRef, pinnedRef string) {
	rule := &Rule{
		Action: PolicyActionConvert,
		Selector: &Selector{
			Identifier: "docker-image://" + originalRef,
			MatchType:  MatchTypeExact,
		},
		Updates: &Update{
			Identifier: "docker-image://" + pinnedRef,
		},
	}
	p.Rules = append(p.Rules, rule)
}

// AddHTTPChecksumRule adds a rule that pins an HTTP/HTTPS source to a specific checksum
// The checksum should be in the format "sha256:..." or similar digest format
func AddHTTPChecksumRule(p *Policy, url, checksum string) {
	rule := &Rule{
		Action: PolicyActionConvert,
		Selector: &Selector{
			Identifier: url,
			MatchType:  MatchTypeExact,
		},
		Updates: &Update{
			Attrs: map[string]string{
				"http.checksum": checksum,
			},
		},
	}
	p.Rules = append(p.Rules, rule)
}

// Validate checks that the policy is valid by performing a JSON round-trip
// through the BuildKit sourcepolicy/pb types. This is the same validation
// that BuildKit performs when loading a policy file via json.Unmarshal.
func Validate(p *Policy) error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var validated spb.Policy
	return json.Unmarshal(data, &validated)
}

// ValidateWithEvaluate performs deeper validation by running each rule through
// BuildKit's sourcepolicy engine. This tests that the generated rules can actually
// be evaluated against source operations, providing runtime validation beyond
// structural correctness.
func ValidateWithEvaluate(ctx context.Context, p *Policy) error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	engine := sourcepolicy.NewEngine([]*spb.Policy{p})
	for i, rule := range p.Rules {
		if rule == nil || rule.Selector == nil {
			return fmt.Errorf("rule %d has nil selector", i)
		}
		op := &pb.SourceOp{
			Identifier: rule.Selector.Identifier,
		}
		if _, err := engine.Evaluate(ctx, op); err != nil {
			return err
		}
	}
	return nil
}
