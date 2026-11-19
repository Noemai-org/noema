package trustport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// ErrProfileNotFound is returned for an intent node the pack does not have, or that has no trust profile. ErrProfilesData is returned by NewPackProfiles for pack data that cannot
// give a sound answer.
var (
	ErrProfileNotFound = errors.New("trustport: no trust profile governs this intent node")
	ErrProfilesData    = errors.New("trustport: the trust profile data is not usable")
)

// PackProfiles is a stub of the red commit.
type PackProfiles struct{}

// NewPackProfiles is a stub of the red commit.
func NewPackProfiles(_ []contracts.IntentNodeV1, _ []contracts.TrustProfileV1) (PackProfiles, error) {
	return PackProfiles{}, nil
}

// SourceRules is a stub of the red commit.
func (PackProfiles) SourceRules(_ context.Context, _, _ string) (SourceRules, error) {
	return SourceRules{}, nil
}

var _ SourceProfiles = PackProfiles{}
