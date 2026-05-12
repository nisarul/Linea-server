// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"

	"github.com/nisarul/Linea-core/model"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/platform"
)

// PersonsService implements pb.PersonsServer.
type PersonsService struct {
	pb.UnimplementedPersonsServer
	resolver
}

// NewPersonsService constructs the service with the supplied
// platform/authz/tenants infrastructure.
func NewPersonsService(p *platformDeps) *PersonsService {
	return &PersonsService{resolver: p.resolver()}
}

func (s *PersonsService) GetPerson(ctx context.Context, req *pb.GetPersonRequest) (*pb.GetPersonResponse, error) {
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
	p, err := rtx.GetPerson(id)
	if err != nil {
		return nil, err
	}
	return &pb.GetPersonResponse{
		Person:       personToProto(p),
		GraphVersion: uint64(rtx.Version()),
	}, nil
}

func (s *PersonsService) ListPersons(ctx context.Context, req *pb.ListPersonsRequest) (*pb.ListPersonsResponse, error) {
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
	out := &pb.ListPersonsResponse{GraphVersion: uint64(rtx.Version())}
	err = rtx.IteratePersons(func(p model.Person) bool {
		out.Persons = append(out.Persons, personToProto(p))
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RelationshipsService implements pb.RelationshipsServer.
type RelationshipsService struct {
	pb.UnimplementedRelationshipsServer
	resolver
}

func NewRelationshipsService(p *platformDeps) *RelationshipsService {
	return &RelationshipsService{resolver: p.resolver()}
}

func (s *RelationshipsService) GetRelationship(ctx context.Context, req *pb.GetRelationshipRequest) (*pb.GetRelationshipResponse, error) {
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
	r, err := rtx.GetRelationship(id)
	if err != nil {
		return nil, err
	}
	return &pb.GetRelationshipResponse{
		Relationship: relationshipToProto(r),
		GraphVersion: uint64(rtx.Version()),
	}, nil
}

func (s *RelationshipsService) ListRelationships(ctx context.Context, req *pb.ListRelationshipsRequest) (*pb.ListRelationshipsResponse, error) {
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
	out := &pb.ListRelationshipsResponse{GraphVersion: uint64(rtx.Version())}
	err = rtx.IterateRelationships(func(r model.Relationship) bool {
		out.Relationships = append(out.Relationships, relationshipToProto(r))
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SourcesService implements pb.SourcesServer.
type SourcesService struct {
	pb.UnimplementedSourcesServer
	resolver
}

func NewSourcesService(p *platformDeps) *SourcesService {
	return &SourcesService{resolver: p.resolver()}
}

func (s *SourcesService) GetSource(ctx context.Context, req *pb.GetSourceRequest) (*pb.GetSourceResponse, error) {
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
	src, err := rtx.GetSource(id)
	if err != nil {
		return nil, err
	}
	return &pb.GetSourceResponse{
		Source:       sourceToProto(src),
		GraphVersion: uint64(rtx.Version()),
	}, nil
}

func (s *SourcesService) ListSources(ctx context.Context, req *pb.ListSourcesRequest) (*pb.ListSourcesResponse, error) {
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
	out := &pb.ListSourcesResponse{GraphVersion: uint64(rtx.Version())}
	err = rtx.IterateSources(func(src model.Source) bool {
		out.Sources = append(out.Sources, sourceToProto(src))
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
