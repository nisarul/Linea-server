// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"

	"github.com/nisarul/Linea-core/model"
	"github.com/nisarul/Linea-core/store"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/tenancy"
)

// PersonsService implements pb.PersonsServer.
type PersonsService struct {
	pb.UnimplementedPersonsServer
	Store store.Store
}

// GetPerson returns a single Person by ID.
func (s *PersonsService) GetPerson(ctx context.Context, req *pb.GetPersonRequest) (*pb.GetPersonResponse, error) {
	_ = tenancy.TenantOf(ctx) // v0.2 will scope per tenant
	id, err := idFromProto(req.GetId())
	if err != nil {
		return nil, err
	}
	rtx, err := s.Store.View(ctx)
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

// ListPersons enumerates all persons. Pagination is currently
// best-effort: the underlying store provides no cursor, so v0.1
// returns the full set in a single page. v0.2 will switch to a
// real cursor when the store grows that capability.
func (s *PersonsService) ListPersons(ctx context.Context, _ *pb.ListPersonsRequest) (*pb.ListPersonsResponse, error) {
	_ = tenancy.TenantOf(ctx)
	rtx, err := s.Store.View(ctx)
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
	Store store.Store
}

func (s *RelationshipsService) GetRelationship(ctx context.Context, req *pb.GetRelationshipRequest) (*pb.GetRelationshipResponse, error) {
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
	r, err := rtx.GetRelationship(id)
	if err != nil {
		return nil, err
	}
	return &pb.GetRelationshipResponse{
		Relationship: relationshipToProto(r),
		GraphVersion: uint64(rtx.Version()),
	}, nil
}

func (s *RelationshipsService) ListRelationships(ctx context.Context, _ *pb.ListRelationshipsRequest) (*pb.ListRelationshipsResponse, error) {
	_ = tenancy.TenantOf(ctx)
	rtx, err := s.Store.View(ctx)
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
	Store store.Store
}

func (s *SourcesService) GetSource(ctx context.Context, req *pb.GetSourceRequest) (*pb.GetSourceResponse, error) {
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
	src, err := rtx.GetSource(id)
	if err != nil {
		return nil, err
	}
	return &pb.GetSourceResponse{
		Source:       sourceToProto(src),
		GraphVersion: uint64(rtx.Version()),
	}, nil
}

func (s *SourcesService) ListSources(ctx context.Context, _ *pb.ListSourcesRequest) (*pb.ListSourcesResponse, error) {
	_ = tenancy.TenantOf(ctx)
	rtx, err := s.Store.View(ctx)
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
