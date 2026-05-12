// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"
	"time"

	"github.com/nisarul/Linea-core/governance"
	"github.com/nisarul/Linea-core/model"
	"github.com/nisarul/Linea-core/store"
	lerrors "github.com/nisarul/Linea-core/errors"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/platform"
)

// ProposalsService implements pb.ProposalsServer.
type ProposalsService struct {
	pb.UnimplementedProposalsServer
	resolver
	proposalsDepsRef *platformDeps
}

func NewProposalsService(p *platformDeps) *ProposalsService {
	return &ProposalsService{resolver: p.resolver(), proposalsDepsRef: p}
}

func (s *ProposalsService) GetProposal(ctx context.Context, req *pb.GetProposalRequest) (*pb.GetProposalResponse, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		platform.RoleViewer, false)
	if err != nil {
		return nil, err
	}
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	rtx, err := st.View(ctx)
	if err != nil {
		return nil, err
	}
	defer rtx.Close()
	p, err := rtx.GetProposal(id)
	if err != nil {
		return nil, err
	}
	return &pb.GetProposalResponse{
		Proposal:     proposalToProto(p),
		GraphVersion: uint64(rtx.Version()),
	}, nil
}

func (s *ProposalsService) ListProposals(ctx context.Context, req *pb.ListProposalsRequest) (*pb.ListProposalsResponse, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		platform.RoleViewer, false)
	if err != nil {
		return nil, err
	}
	rtx, err := st.View(ctx)
	if err != nil {
		return nil, err
	}
	defer rtx.Close()
	filter := proposalStateFromProto(req.GetStateFilter())
	out := &pb.ListProposalsResponse{GraphVersion: uint64(rtx.Version())}
	err = rtx.IterateProposals(func(p model.Proposal) bool {
		if filter != 0 && p.State() != filter {
			return true
		}
		out.Proposals = append(out.Proposals, proposalToProto(p))
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ProposalsService) CreateProposal(ctx context.Context, req *pb.CreateProposalRequest) (*pb.CreateProposalResponse, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		platform.RoleContributor, true)
	if err != nil {
		return nil, err
	}
	id := auth.IdentityOf(ctx)
	target, err := idFromProto(req.GetTargetId())
	if err != nil {
		return nil, err
	}
	secondary, err := idFromProto(req.GetSecondaryId())
	if err != nil {
		return nil, err
	}
	srcIDs := make([]model.ID, 0, len(req.GetSources()))
	for _, sid := range req.GetSources() {
		mid, err := idFromProto(sid)
		if err != nil {
			return nil, err
		}
		srcIDs = append(srcIDs, mid)
	}
	pp, err := model.NewProposal(model.NewID(),
		actionFromProto(req.GetAction()),
		entityKindFromProto(req.GetEntityKind()),
		model.ProposalOptions{
			TargetID:    target,
			SecondaryID: secondary,
			Payload:     req.GetPayload(),
			Reason:      req.GetReason(),
			Sources:     srcIDs,
			Author:      id.Subject,
			CreatedAt:   time.Now().Unix(),
		})
	if err != nil {
		return nil, err
	}
	v, err := st.Update(ctx, func(tx store.WriteTx) error { return tx.PutProposal(pp) })
	if err != nil {
		return nil, err
	}
	return &pb.CreateProposalResponse{
		Proposal:     proposalToProto(pp),
		GraphVersion: uint64(v),
	}, nil
}

// ----- transitions -----
//
// Author submit/withdraw need only Contributor; curator
// transitions (claim/accept/reject) need Curator.

func (s *ProposalsService) Submit(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transition(ctx, req, platform.RoleContributor, true, governance.Submit)
}
func (s *ProposalsService) Withdraw(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transitionWithReason(ctx, req, platform.RoleContributor, false, governance.Withdraw)
}
func (s *ProposalsService) Claim(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transition(ctx, req, platform.RoleCurator, false, governance.Claim)
}
func (s *ProposalsService) Accept(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	gid := req.GetGenealogyId()
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, gid,
		platform.RoleCurator, false)
	if err != nil {
		return nil, err
	}
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	// Person quota: if this proposal would create a Person, check
	// before applying so we never exceed the cap.
	rtx, err := st.View(ctx)
	if err != nil {
		return nil, err
	}
	pendingProp, err := rtx.GetProposal(id)
	rtx.Close()
	if err != nil {
		return nil, err
	}
	createsPerson := pendingProp.Action() == model.ProposalActionCreate &&
		pendingProp.EntityKind() == model.EntityKindPerson
	if createsPerson {
		if err := s.qcheckCreatePerson(gid); err != nil {
			return nil, err
		}
	}
	actor := auth.IdentityOf(ctx).Subject
	out, err := governance.Accept(ctx, st, id, actor, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if createsPerson {
		_ = s.qincPerson(gid)
	}
	return proposalToProto(out), nil
}

// qcheckCreatePerson is a tiny indirection so deps may be nil in
// older tests; production wiring always sets Quotas.
func (s *ProposalsService) qcheckCreatePerson(gid string) error {
	d := s.deps()
	if d == nil || d.Quotas == nil {
		return nil
	}
	if err := d.Quotas.CheckCreatePerson(gid); err != nil {
		return lerrors.New(lerrors.CodeInvalidArgument, "person quota exceeded")
	}
	return nil
}

func (s *ProposalsService) qincPerson(gid string) error {
	d := s.deps()
	if d == nil || d.Quotas == nil {
		return nil
	}
	return d.Quotas.IncPerson(gid)
}

// deps recovers the platformDeps the resolver was built from.
// resolver embeds platform/authz/tenants but not Quotas; we keep
// a back-reference here. v0.3 will refactor this away.
func (s *ProposalsService) deps() *platformDeps {
	return s.proposalsDepsRef
}
func (s *ProposalsService) Reject(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transitionWithReason(ctx, req, platform.RoleCurator, false, governance.Reject)
}

// BulkReject runs Reject for each id, collecting per-id results.
// All ids must belong to the same genealogy. Curator role is
// enforced once via the resolver; per-proposal Reject errors
// (e.g. terminal-state) are reported per id rather than aborting.
func (s *ProposalsService) BulkReject(ctx context.Context, req *pb.BulkRejectRequest) (*pb.BulkRejectResponse, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		platform.RoleCurator, false)
	if err != nil {
		return nil, err
	}
	reason := req.GetReason()
	if reason == "" {
		reason = "bulk reject"
	}
	actor := auth.IdentityOf(ctx).Subject
	now := time.Now().Unix()
	out := &pb.BulkRejectResponse{}
	for _, idProto := range req.GetIds() {
		id, err := idFromProto(idProto)
		if err != nil {
			out.Results = append(out.Results, &pb.BulkRejectResult{
				Id: idProto, Ok: false, Error: err.Error(),
			})
			continue
		}
		_, err = governance.Reject(ctx, st, id, actor, now, reason)
		if err != nil {
			out.Results = append(out.Results, &pb.BulkRejectResult{
				Id: idProto, Ok: false, Error: err.Error(),
			})
			continue
		}
		out.Results = append(out.Results, &pb.BulkRejectResult{Id: idProto, Ok: true})
	}
	return out, nil
}

func (s *ProposalsService) transition(
	ctx context.Context, req *pb.TransitionRequest,
	minRole platform.Role, requiresProposalSubmission bool,
	fn func(context.Context, store.Store, model.ID, string, int64) (model.Proposal, error),
) (*pb.Proposal, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		minRole, requiresProposalSubmission)
	if err != nil {
		return nil, err
	}
	actor := auth.IdentityOf(ctx).Subject
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	out, err := fn(ctx, st, id, actor, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return proposalToProto(out), nil
}

func (s *ProposalsService) transitionWithReason(
	ctx context.Context, req *pb.TransitionRequest,
	minRole platform.Role, requiresProposalSubmission bool,
	fn func(context.Context, store.Store, model.ID, string, int64, string) (model.Proposal, error),
) (*pb.Proposal, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		minRole, requiresProposalSubmission)
	if err != nil {
		return nil, err
	}
	actor := auth.IdentityOf(ctx).Subject
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	out, err := fn(ctx, st, id, actor, time.Now().Unix(), req.GetReason())
	if err != nil {
		return nil, err
	}
	return proposalToProto(out), nil
}
