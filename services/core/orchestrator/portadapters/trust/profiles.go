package trustport

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Errors of the PackProfiles adapter. Neither names a node, a profile or a source of the pack (AGENT-13). Callers compare with errors.Is.
var (
	// ErrProfileNotFound is returned for an intent node the pack does not have, or that has no trust profile: such a node has no evidence rules, and no rules is never an empty set.
	ErrProfileNotFound = errors.New("trustport: no trust profile governs this intent node")
	// ErrProfilesData is returned by NewPackProfiles for pack data that cannot give a sound answer.
	ErrProfilesData = errors.New("trustport: the trust profile data is not usable")
)

// PackProfiles implements SourceProfiles over the pack's data (trust-profile.v1: `evidence_required` and `max_evidence_age_seconds`). It is built once, read-only after that and safe
// to share. The pack is data, the same for every tenant: no node, profile or source ID is written here (INV-14, SoT 51), and no number is chosen here (AGENT-16). The zero value is
// not a pack: it refuses every question (INV-16).
type PackProfiles struct {
	rules map[string]SourceRules
}

var _ SourceProfiles = PackProfiles{}

func profilesData(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrProfilesData, fmt.Sprintf(format, args...))
}

// NewPackProfiles reads the evidence rules of each intent node from the node's trust profile. It refuses the whole pack, with ErrProfilesData, when:
//   - there is no intent node, or a node has no ID or is repeated;
//   - a trust profile has no ID or is repeated;
//   - a node names a trust profile the pack does not have;
//   - a profile's `max_evidence_age_seconds` is below 1 (a profile may only tighten a source's limit, INV-23, and a limit of nothing tightens to nothing);
//   - a profile requires no source, a source with no ID, or the same source twice.
//
// A node with no trust profile is allowed (it is out of scope) and has no rules: SourceRules answers ErrProfileNotFound for it. The error names the rule, never the content.
func NewPackProfiles(nodes []contracts.IntentNodeV1, profiles []contracts.TrustProfileV1) (PackProfiles, error) {
	if len(nodes) == 0 {
		return PackProfiles{}, profilesData("the pack has no intent node")
	}
	byProfile := make(map[string]SourceRules, len(profiles))
	for _, p := range profiles {
		id := string(p.ProfileId)
		if id == "" {
			return PackProfiles{}, profilesData("a trust profile has no ID")
		}
		if _, dup := byProfile[id]; dup {
			return PackProfiles{}, profilesData("a trust profile is repeated")
		}
		r, err := rulesOf(p)
		if err != nil {
			return PackProfiles{}, err
		}
		byProfile[id] = r
	}
	rules := make(map[string]SourceRules, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		id := string(n.NodeId)
		if id == "" {
			return PackProfiles{}, profilesData("an intent node has no ID")
		}
		if seen[id] {
			return PackProfiles{}, profilesData("an intent node is repeated")
		}
		seen[id] = true
		if n.TrustProfile == nil {
			continue
		}
		r, ok := byProfile[*n.TrustProfile]
		if !ok {
			return PackProfiles{}, profilesData("an intent node names a trust profile the pack does not have")
		}
		rules[id] = r
	}
	return PackProfiles{rules: rules}, nil
}

func rulesOf(p contracts.TrustProfileV1) (SourceRules, error) {
	if p.MaxEvidenceAgeSeconds < 1 {
		return SourceRules{}, profilesData("a trust profile's evidence age limit is below one second")
	}
	if len(p.EvidenceRequired) == 0 {
		return SourceRules{}, profilesData("a trust profile requires no evidence source")
	}
	required := make([]string, len(p.EvidenceRequired))
	seen := make(map[string]bool, len(p.EvidenceRequired))
	for i, s := range p.EvidenceRequired {
		src := string(s)
		if src == "" || seen[src] {
			return SourceRules{}, profilesData("a trust profile's required sources are empty or repeated")
		}
		seen[src] = true
		required[i] = src
	}
	return SourceRules{RequiredSources: required, MaxAgeSeconds: p.MaxEvidenceAgeSeconds}, nil
}

// SourceRules gives the evidence rules of the profile that governs intentNode, as a copy the caller may keep or change. tenantID must have the shape of a tenant ID; the pack is the
// same for every tenant. A node with no profile is ErrProfileNotFound; a zero value is ErrConfig; a context that is done answers its own error. On any error the rules are empty.
func (p PackProfiles) SourceRules(ctx context.Context, tenantID, intentNode string) (SourceRules, error) {
	if p.rules == nil {
		return SourceRules{}, ErrConfig
	}
	if !chain.ValidTenantID(tenantID) {
		return SourceRules{}, ErrSession
	}
	if err := ctx.Err(); err != nil {
		return SourceRules{}, err
	}
	r, ok := p.rules[intentNode]
	if !ok {
		return SourceRules{}, ErrProfileNotFound
	}
	return SourceRules{RequiredSources: append([]string(nil), r.RequiredSources...), MaxAgeSeconds: r.MaxAgeSeconds}, nil
}
