// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	lerrors "github.com/nisarul/Linea-core/errors"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/authz"
	"github.com/nisarul/Linea-server/internal/platform"
	"github.com/nisarul/Linea-server/internal/tenants"
)

// GenealogiesService implements pb.GenealogiesServer. It owns
// the lifecycle of Genealogy records, memberships, and bans.
type GenealogiesService struct {
	pb.UnimplementedGenealogiesServer
	deps *platformDeps
}

func NewGenealogiesService(p *platformDeps) *GenealogiesService {
	return &GenealogiesService{deps: p}
}

// CreateGenealogy. Defaults to PRIVATE. The caller becomes Owner.
func (s *GenealogiesService) CreateGenealogy(ctx context.Context, req *pb.CreateGenealogyRequest) (*pb.CreateGenealogyResponse, error) {
	id := auth.IdentityOf(ctx)
	if id.Subject == "" {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "authentication required")
	}
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "name is required")
	}
	vis := visibilityFromProto(req.GetVisibility())
	if vis == 0 {
		vis = platform.VisibilityPrivate
	}

	// Quota: only Private genealogies count against the per-user limit.
	if vis == platform.VisibilityPrivate {
		if err := s.deps.Quotas.CheckCreatePrivateGenealogy(id.Subject); err != nil {
			return nil, lerrors.New(lerrors.CodeInvalidArgument,
				"private-genealogy quota exceeded")
		}
	}

	gid := uuid.NewString()
	g := platform.Genealogy{
		ID:         gid,
		Name:       name,
		Visibility: vis,
		CreatedBy:  id.Subject,
		CreatedAt:  platform.Now(),
	}
	if err := s.deps.Platform.PutGenealogy(g); err != nil {
		return nil, err
	}
	if err := s.deps.Platform.PutMembership(platform.Membership{
		Subject: id.Subject, GenealogyID: gid, Role: platform.RoleOwner,
		GrantedBy: id.Subject, GrantedAt: platform.Now(),
	}); err != nil {
		return nil, err
	}
	if vis == platform.VisibilityPrivate {
		if err := s.deps.Quotas.IncPrivateGenealogy(id.Subject); err != nil {
			return nil, err
		}
	}
	s.deps.Authz.Invalidate(id.Subject, gid)
	return &pb.CreateGenealogyResponse{
		Genealogy: genealogyToProto(g, platform.RoleOwner),
	}, nil
}

// ListGenealogies returns Genealogies the caller can see:
//   - Public ones (always)
//   - Private/Unlisted ones the caller is an explicit member of
func (s *GenealogiesService) ListGenealogies(ctx context.Context, _ *pb.ListGenealogiesRequest) (*pb.ListGenealogiesResponse, error) {
	id := auth.IdentityOf(ctx)
	out := &pb.ListGenealogiesResponse{}
	seen := make(map[string]struct{})
	addUnique := func(g platform.Genealogy, role platform.Role) {
		if _, ok := seen[g.ID]; ok {
			return
		}
		seen[g.ID] = struct{}{}
		out.Genealogies = append(out.Genealogies, genealogyToProto(g, role))
	}

	// Memberships first (gives us the strongest role).
	if id.Subject != "" {
		err := s.deps.Platform.IterateMembershipsForUser(id.Subject, func(m platform.Membership) bool {
			g, err := s.deps.Platform.GetGenealogy(m.GenealogyID)
			if err != nil {
				return true // skip dangling
			}
			addUnique(g, m.Role)
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	// Then Public ones (implicit role).
	err := s.deps.Platform.IterateGenealogies(func(g platform.Genealogy) bool {
		if g.Visibility != platform.VisibilityPublic {
			return true
		}
		role := platform.RoleViewer
		if id.Subject != "" {
			role = platform.RoleContributor
		}
		addUnique(g, role)
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetGenealogy. Returns NotFound if the caller has no role.
func (s *GenealogiesService) GetGenealogy(ctx context.Context, req *pb.GetGenealogyRequest) (*pb.GetGenealogyResponse, error) {
	id := auth.IdentityOf(ctx)
	d, err := s.deps.Authz.Resolve(ctx, id.Subject, req.GetId())
	if err != nil {
		return nil, mapAuthzError(err)
	}
	g, err := s.deps.Platform.GetGenealogy(req.GetId())
	if err != nil {
		return nil, err
	}
	return &pb.GetGenealogyResponse{Genealogy: genealogyToProto(g, d.Role)}, nil
}

// UpdateVisibility. Owner only.
func (s *GenealogiesService) UpdateVisibility(ctx context.Context, req *pb.UpdateVisibilityRequest) (*pb.Genealogy, error) {
	id := auth.IdentityOf(ctx)
	if err := s.requireRole(ctx, id.Subject, req.GetId(), platform.RoleOwner); err != nil {
		return nil, err
	}
	vis := visibilityFromProto(req.GetVisibility())
	if vis == 0 {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "visibility is required")
	}
	g, err := s.deps.Platform.GetGenealogy(req.GetId())
	if err != nil {
		return nil, err
	}
	prev := g.Visibility
	g.Visibility = vis
	// Quota: if becoming Private, ensure the owner is under their cap
	// (the genealogy's first owner is treated as the quota holder).
	if prev != platform.VisibilityPrivate && vis == platform.VisibilityPrivate {
		if err := s.deps.Quotas.CheckCreatePrivateGenealogy(g.CreatedBy); err != nil {
			return nil, lerrors.New(lerrors.CodeInvalidArgument,
				"private-genealogy quota exceeded")
		}
	}
	if err := s.deps.Platform.PutGenealogy(g); err != nil {
		return nil, err
	}
	// Adjust counter on the visibility transition.
	switch {
	case prev == platform.VisibilityPrivate && vis != platform.VisibilityPrivate:
		_ = s.deps.Quotas.DecPrivateGenealogy(g.CreatedBy)
	case prev != platform.VisibilityPrivate && vis == platform.VisibilityPrivate:
		_ = s.deps.Quotas.IncPrivateGenealogy(g.CreatedBy)
	}
	s.deps.Authz.Invalidate("", req.GetId())
	return genealogyToProto(g, platform.RoleOwner), nil
}

// DeleteGenealogy. Owner only. Closes per-tenant store + drops record.
func (s *GenealogiesService) DeleteGenealogy(ctx context.Context, req *pb.DeleteGenealogyRequest) (*pb.DeleteGenealogyResponse, error) {
	id := auth.IdentityOf(ctx)
	if err := s.requireRole(ctx, id.Subject, req.GetId(), platform.RoleOwner); err != nil {
		return nil, err
	}
	g, err := s.deps.Platform.GetGenealogy(req.GetId())
	if err != nil {
		return nil, err
	}
	// Drop in-memory handle and remove the on-disk DB.
	_ = s.deps.Tenants.Drop(req.GetId())
	if err := s.deps.Tenants.RemoveOnDisk(req.GetId()); err != nil {
		return nil, fmt.Errorf("delete on-disk: %w", err)
	}
	// Remove memberships + bans before the record itself.
	var mems []string
	if err := s.deps.Platform.IterateMembershipsForGenealogy(req.GetId(), func(m platform.Membership) bool {
		mems = append(mems, m.Subject)
		return true
	}); err != nil {
		return nil, err
	}
	for _, sub := range mems {
		_ = s.deps.Platform.DeleteMembership(req.GetId(), sub)
	}
	if err := s.deps.Platform.DeleteGenealogy(req.GetId()); err != nil {
		return nil, err
	}
	if g.Visibility == platform.VisibilityPrivate {
		_ = s.deps.Quotas.DecPrivateGenealogy(g.CreatedBy)
	}
	s.deps.Authz.Invalidate("", req.GetId())
	return &pb.DeleteGenealogyResponse{}, nil
}

// UpsertMembership. Owner can grant any role; Curator can grant
// up to Curator. Granting Owner requires Owner.
func (s *GenealogiesService) UpsertMembership(ctx context.Context, req *pb.UpsertMembershipRequest) (*pb.Membership, error) {
	id := auth.IdentityOf(ctx)
	d, err := s.deps.Authz.Resolve(ctx, id.Subject, req.GetGenealogyId())
	if err != nil {
		return nil, mapAuthzError(err)
	}
	if d.Role < platform.RoleCurator {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "Curator or Owner role required")
	}
	target := strings.TrimSpace(req.GetSubject())
	role := roleFromProto(req.GetRole())
	if !role.IsValid() {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "invalid role")
	}
	if role == platform.RoleOwner && d.Role < platform.RoleOwner {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "only an Owner may grant Owner")
	}
	m := platform.Membership{
		Subject: target, GenealogyID: req.GetGenealogyId(),
		Role: role, GrantedBy: id.Subject, GrantedAt: platform.Now(),
	}
	if err := s.deps.Platform.PutMembership(m); err != nil {
		return nil, err
	}
	s.deps.Authz.Invalidate(target, req.GetGenealogyId())
	return membershipToProto(m), nil
}

// RemoveMember. Curator+ may remove any non-Owner; Owner required
// to remove an Owner. Removing the LAST Owner is forbidden.
func (s *GenealogiesService) RemoveMember(ctx context.Context, req *pb.RemoveMemberRequest) (*pb.RemoveMemberResponse, error) {
	id := auth.IdentityOf(ctx)
	d, err := s.deps.Authz.Resolve(ctx, id.Subject, req.GetGenealogyId())
	if err != nil {
		return nil, mapAuthzError(err)
	}
	if d.Role < platform.RoleCurator {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "Curator or Owner role required")
	}
	target := strings.TrimSpace(req.GetSubject())
	existing, err := s.deps.Platform.GetMembership(req.GetGenealogyId(), target)
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			return &pb.RemoveMemberResponse{}, nil
		}
		return nil, err
	}
	if existing.Role == platform.RoleOwner {
		if d.Role < platform.RoleOwner {
			return nil, lerrors.New(lerrors.CodeInvalidArgument, "only an Owner may remove an Owner")
		}
		owners, err := s.countOwners(req.GetGenealogyId())
		if err != nil {
			return nil, err
		}
		if owners <= 1 {
			return nil, lerrors.New(lerrors.CodeInvalidArgument,
				"cannot remove the last Owner; transfer ownership or delete the genealogy")
		}
	}
	if err := s.deps.Platform.DeleteMembership(req.GetGenealogyId(), target); err != nil {
		return nil, err
	}
	s.deps.Authz.Invalidate(target, req.GetGenealogyId())
	return &pb.RemoveMemberResponse{}, nil
}

// LeaveGenealogy: self-removal. Owners blocked unless another
// Owner remains.
func (s *GenealogiesService) LeaveGenealogy(ctx context.Context, req *pb.LeaveGenealogyRequest) (*pb.LeaveGenealogyResponse, error) {
	id := auth.IdentityOf(ctx)
	if id.Subject == "" {
		return nil, lerrors.New(lerrors.CodeInvalidArgument, "authentication required")
	}
	existing, err := s.deps.Platform.GetMembership(req.GetId(), id.Subject)
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			return &pb.LeaveGenealogyResponse{}, nil
		}
		return nil, err
	}
	if existing.Role == platform.RoleOwner {
		owners, err := s.countOwners(req.GetId())
		if err != nil {
			return nil, err
		}
		if owners <= 1 {
			return nil, lerrors.New(lerrors.CodeInvalidArgument,
				"the last Owner cannot leave; transfer ownership or delete the genealogy")
		}
	}
	if err := s.deps.Platform.DeleteMembership(req.GetId(), id.Subject); err != nil {
		return nil, err
	}
	s.deps.Authz.Invalidate(id.Subject, req.GetId())
	return &pb.LeaveGenealogyResponse{}, nil
}

func (s *GenealogiesService) BanUser(ctx context.Context, req *pb.BanUserRequest) (*pb.Ban, error) {
	id := auth.IdentityOf(ctx)
	if err := s.requireRole(ctx, id.Subject, req.GetGenealogyId(), platform.RoleCurator); err != nil {
		return nil, err
	}
	b := platform.Ban{
		Subject: req.GetSubject(), GenealogyID: req.GetGenealogyId(),
		Reason: req.GetReason(), By: id.Subject, At: platform.Now(),
	}
	if err := s.deps.Platform.PutBan(b); err != nil {
		return nil, err
	}
	return banToProto(b), nil
}

func (s *GenealogiesService) UnbanUser(ctx context.Context, req *pb.UnbanUserRequest) (*pb.UnbanUserResponse, error) {
	id := auth.IdentityOf(ctx)
	if err := s.requireRole(ctx, id.Subject, req.GetGenealogyId(), platform.RoleCurator); err != nil {
		return nil, err
	}
	if err := s.deps.Platform.DeleteBan(req.GetGenealogyId(), req.GetSubject()); err != nil {
		return nil, err
	}
	return &pb.UnbanUserResponse{}, nil
}

func (s *GenealogiesService) ListMembers(ctx context.Context, req *pb.ListMembersRequest) (*pb.ListMembersResponse, error) {
	id := auth.IdentityOf(ctx)
	if err := s.requireRole(ctx, id.Subject, req.GetGenealogyId(), platform.RoleViewer); err != nil {
		return nil, err
	}
	out := &pb.ListMembersResponse{}
	err := s.deps.Platform.IterateMembershipsForGenealogy(req.GetGenealogyId(), func(m platform.Membership) bool {
		out.Memberships = append(out.Memberships, membershipToProto(m))
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ----- helpers -----

func (s *GenealogiesService) requireRole(ctx context.Context, sub, gid string, min platform.Role) error {
	d, err := s.deps.Authz.Resolve(ctx, sub, gid)
	if err != nil {
		return mapAuthzError(err)
	}
	if d.Role < min {
		return lerrors.New(lerrors.CodeInvalidArgument,
			"insufficient role: have "+d.Role.String()+", need "+min.String())
	}
	return nil
}

func (s *GenealogiesService) countOwners(gid string) (int, error) {
	n := 0
	err := s.deps.Platform.IterateMembershipsForGenealogy(gid, func(m platform.Membership) bool {
		if m.Role == platform.RoleOwner {
			n++
		}
		return true
	})
	return n, err
}

func mapAuthzError(err error) error {
	if errors.Is(err, authz.ErrNotAllowed) {
		return lerrors.New(lerrors.CodePersonNotFound,
			"genealogy not found or access denied")
	}
	return err
}

// Compile-time check we satisfy the gRPC interface.
var _ pb.GenealogiesServer = (*GenealogiesService)(nil)

// Suppress "imported and not used" if tenants pkg becomes unused
// after refactors.
var _ = tenants.ErrShutdown
