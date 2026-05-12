// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"

	"github.com/nisarul/Linea-core/explain"
	"github.com/nisarul/Linea-core/query"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/platform"
)

// QueriesService implements pb.QueriesServer.
type QueriesService struct {
	pb.UnimplementedQueriesServer
	resolver
}

func NewQueriesService(p *platformDeps) *QueriesService {
	return &QueriesService{resolver: p.resolver()}
}

func (s *QueriesService) FindPaths(ctx context.Context, req *pb.FindPathsRequest) (*pb.FindPathsResponse, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		platform.RoleViewer, false)
	if err != nil {
		return nil, err
	}
	from, err := idFromProto(req.GetFrom())
	if err != nil {
		return nil, err
	}
	to, err := idFromProto(req.GetTo())
	if err != nil {
		return nil, err
	}
	rtx, err := st.View(ctx)
	if err != nil {
		return nil, err
	}
	defer rtx.Close()
	paths, err := query.FindPaths(ctx, rtx, from, to, query.Options{
		MaxDepth:       int(req.GetMaxDepth()),
		MaxPaths:       int(req.GetMaxPaths()),
		IncludeAffinal: req.GetIncludeAffinal(),
	})
	if err != nil {
		return nil, err
	}
	out := &pb.FindPathsResponse{GraphVersion: uint64(rtx.Version())}
	for _, p := range paths {
		exp, err := explain.Path(rtx, p)
		if err != nil {
			return nil, err
		}
		out.Paths = append(out.Paths, pathExplanationToProto(exp))
	}
	return out, nil
}

func (s *QueriesService) NKCA(ctx context.Context, req *pb.NKCARequest) (*pb.NKCAResponse, error) {
	st, _, err := s.resolveStore(ctx, auth.IdentityOf(ctx).Subject, req.GetGenealogyId(),
		platform.RoleViewer, false)
	if err != nil {
		return nil, err
	}
	a, err := idFromProto(req.GetA())
	if err != nil {
		return nil, err
	}
	b, err := idFromProto(req.GetB())
	if err != nil {
		return nil, err
	}
	rtx, err := st.View(ctx)
	if err != nil {
		return nil, err
	}
	defer rtx.Close()
	res, err := query.NearestKnownCommonAncestor(ctx, rtx, a, b, query.Options{})
	if err != nil {
		return nil, err
	}
	exp, err := explain.CommonAncestor(rtx, res)
	if err != nil {
		return nil, err
	}
	out := &pb.NKCAResponse{
		AncestorId:        &pb.Id{Value: exp.AncestorID.String()},
		AncestorIsUnknown: exp.AncestorIsUnknown,
		TotalGenerations:  int32(exp.TotalGenerations),
		CombinedCertainty: certaintyToProto(exp.CombinedCertainty),
		GraphVersion:      uint64(exp.GraphVersion),
	}
	if exp.PathFromA != nil {
		out.PathFromA = pathExplanationToProto(*exp.PathFromA)
	}
	if exp.PathFromB != nil {
		out.PathFromB = pathExplanationToProto(*exp.PathFromB)
	}
	return out, nil
}

func pathExplanationToProto(exp explain.PathExplanation) *pb.Path {
	out := &pb.Path{
		From:                idToProto(exp.From),
		To:                  idToProto(exp.To),
		Length:              int32(exp.Length),
		Certainty:           certaintyToProto(exp.OverallCertainty),
		TotalGapGenerations: int32(exp.TotalGapGenerations),
		GapEdgeCount:        int32(exp.GapEdgeCount),
		Classification:      classificationToProto(exp.Classification),
		GraphVersion:        uint64(exp.GraphVersion),
	}
	for _, e := range exp.Edges {
		step := &pb.Step{
			FromPerson:     idToProto(e.FromPerson),
			ToPerson:       idToProto(e.ToPerson),
			RelationshipId: idToProto(e.RelationshipID),
			Type:           relTypeToProto(e.Type),
			Certainty:      certaintyToProto(e.Certainty),
			Continuity:     continuityToProto(e.Continuity),
			IsWeakestLink:  e.IsWeakestLink,
		}
		for _, sid := range e.SourceIDs {
			step.Sources = append(step.Sources, idToProto(sid))
		}
		out.Steps = append(out.Steps, step)
	}
	return out
}

func classificationToProto(c query.PathClassification) pb.PathClassification {
	switch c {
	case query.PathLineage:
		return pb.PathClassification_PATH_CLASSIFICATION_LINEAGE
	case query.PathAffinal:
		return pb.PathClassification_PATH_CLASSIFICATION_AFFINAL
	}
	return pb.PathClassification_PATH_CLASSIFICATION_UNSPECIFIED
}
