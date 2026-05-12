// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"
	"errors"

	lerrors "github.com/nisarul/Linea-core/errors"
	"github.com/nisarul/Linea-core/store"

	"github.com/nisarul/Linea-server/internal/authz"
	"github.com/nisarul/Linea-server/internal/platform"
	"github.com/nisarul/Linea-server/internal/tenants"
)

// resolver bundles the platform/tenant lookup paths needed by
// every per-genealogy service handler.
type resolver struct {
	platform *platform.Store
	authz    *authz.Resolver
	tenants  *tenants.Manager
}

// resolveStore enforces visibility/role/ban rules and returns the
// per-genealogy store the handler should operate against.
//
// minRole is the minimum role required to invoke the calling RPC.
// requiresProposalSubmission applies to write RPCs that submit
// proposals — bans block these but not reads.
func (r *resolver) resolveStore(
	ctx context.Context,
	subject, genealogyID string,
	minRole platform.Role,
	requiresProposalSubmission bool,
) (store.Store, authz.Decision, error) {
	if genealogyID == "" {
		return nil, authz.Decision{}, lerrors.New(lerrors.CodeInvalidArgument,
			"genealogy_id is required")
	}
	d, err := r.authz.Resolve(ctx, subject, genealogyID)
	if err != nil {
		if errors.Is(err, authz.ErrNotAllowed) {
			return nil, authz.Decision{}, lerrors.New(lerrors.CodePersonNotFound,
				"genealogy not found or access denied")
		}
		return nil, authz.Decision{}, err
	}
	if d.Role < minRole {
		return nil, authz.Decision{}, lerrors.New(lerrors.CodeInvalidArgument,
			"insufficient role: have "+d.Role.String()+", need "+minRole.String())
	}
	if requiresProposalSubmission && subject != "" {
		banned, err := r.platform.IsBanned(genealogyID, subject)
		if err != nil {
			return nil, authz.Decision{}, err
		}
		if banned {
			return nil, authz.Decision{}, lerrors.New(lerrors.CodeInvalidArgument,
				"banned from this genealogy")
		}
	}
	st, err := r.tenants.Get(ctx, genealogyID)
	if err != nil {
		return nil, authz.Decision{}, err
	}
	return st, d, nil
}
