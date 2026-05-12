// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"
	"time"

	"github.com/nisarul/Linea-core/governance"
	"github.com/nisarul/Linea-core/model"
	"github.com/nisarul/Linea-core/store"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/tenancy"
)

// ProposalsService implements pb.ProposalsServer.
type ProposalsService struct {
	pb.UnimplementedProposalsServer
	Store store.Store
}

func (s *ProposalsService) GetProposal(ctx context.Context, req *pb.GetProposalRequest) (*pb.GetProposalResponse, error) {
	_ = tenancy.TenantOf(ctx)
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	rtx, err := s.Store.View(ctx)
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
	_ = tenancy.TenantOf(ctx)
	rtx, err := s.Store.View(ctx)
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
	_ = tenancy.TenantOf(ctx)
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
	v, err := s.Store.Update(ctx, func(tx store.WriteTx) error { return tx.PutProposal(pp) })
	if err != nil {
		return nil, err
	}
	return &pb.CreateProposalResponse{
		Proposal:     proposalToProto(pp),
		GraphVersion: uint64(v),
	}, nil
}

// ----- transitions -----

func (s *ProposalsService) Submit(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transition(ctx, req, governance.Submit)
}
func (s *ProposalsService) Claim(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transition(ctx, req, governance.Claim)
}
func (s *ProposalsService) Accept(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transition(ctx, req, func(c context.Context, st store.Store, id model.ID, actor string, ts int64) (model.Proposal, error) {
		return governance.Accept(c, st, id, actor, ts)
	})
}
func (s *ProposalsService) Reject(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transitionWithReason(ctx, req, governance.Reject)
}
func (s *ProposalsService) Withdraw(ctx context.Context, req *pb.TransitionRequest) (*pb.Proposal, error) {
	return s.transitionWithReason(ctx, req, governance.Withdraw)
}

// transition runs a no-reason lifecycle move (Submit, Claim, Accept).
func (s *ProposalsService) transition(
	ctx context.Context, req *pb.TransitionRequest,
	fn func(context.Context, store.Store, model.ID, string, int64) (model.Proposal, error),
) (*pb.Proposal, error) {
	_ = tenancy.TenantOf(ctx)
	actor := auth.IdentityOf(ctx).Subject
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	out, err := fn(ctx, s.Store, id, actor, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return proposalToProto(out), nil
}

func (s *ProposalsService) transitionWithReason(
	ctx context.Context, req *pb.TransitionRequest,
	fn func(context.Context, store.Store, model.ID, string, int64, string) (model.Proposal, error),
) (*pb.Proposal, error) {
	_ = tenancy.TenantOf(ctx)
	actor := auth.IdentityOf(ctx).Subject
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	out, err := fn(ctx, s.Store, id, actor, time.Now().Unix(), req.GetReason())
	if err != nil {
		return nil, err
	}
	return proposalToProto(out), nil
}
