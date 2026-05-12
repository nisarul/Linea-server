// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/platform"
)

// ----- Visibility -----

func visibilityToProto(v platform.Visibility) pb.Visibility {
	switch v {
	case platform.VisibilityPrivate:
		return pb.Visibility_VISIBILITY_PRIVATE
	case platform.VisibilityUnlisted:
		return pb.Visibility_VISIBILITY_UNLISTED
	case platform.VisibilityPublic:
		return pb.Visibility_VISIBILITY_PUBLIC
	}
	return pb.Visibility_VISIBILITY_UNSPECIFIED
}

func visibilityFromProto(v pb.Visibility) platform.Visibility {
	switch v {
	case pb.Visibility_VISIBILITY_PRIVATE:
		return platform.VisibilityPrivate
	case pb.Visibility_VISIBILITY_UNLISTED:
		return platform.VisibilityUnlisted
	case pb.Visibility_VISIBILITY_PUBLIC:
		return platform.VisibilityPublic
	}
	return 0
}

// ----- GenealogyRole -----

func roleToProto(r platform.Role) pb.GenealogyRole {
	switch r {
	case platform.RoleNone:
		return pb.GenealogyRole_GENEALOGY_ROLE_NONE
	case platform.RoleViewer:
		return pb.GenealogyRole_GENEALOGY_ROLE_VIEWER
	case platform.RoleContributor:
		return pb.GenealogyRole_GENEALOGY_ROLE_CONTRIBUTOR
	case platform.RoleCurator:
		return pb.GenealogyRole_GENEALOGY_ROLE_CURATOR
	case platform.RoleOwner:
		return pb.GenealogyRole_GENEALOGY_ROLE_OWNER
	}
	return pb.GenealogyRole_GENEALOGY_ROLE_UNSPECIFIED
}

func roleFromProto(r pb.GenealogyRole) platform.Role {
	switch r {
	case pb.GenealogyRole_GENEALOGY_ROLE_VIEWER:
		return platform.RoleViewer
	case pb.GenealogyRole_GENEALOGY_ROLE_CONTRIBUTOR:
		return platform.RoleContributor
	case pb.GenealogyRole_GENEALOGY_ROLE_CURATOR:
		return platform.RoleCurator
	case pb.GenealogyRole_GENEALOGY_ROLE_OWNER:
		return platform.RoleOwner
	}
	return 0
}

// ----- entity mappers -----

func genealogyToProto(g platform.Genealogy, myRole platform.Role) *pb.Genealogy {
	out := &pb.Genealogy{
		Id:         g.ID,
		Name:       g.Name,
		Visibility: visibilityToProto(g.Visibility),
		CreatedBy:  g.CreatedBy,
		MyRole:     roleToProto(myRole),
	}
	if g.CreatedAt > 0 {
		out.CreatedAt = timestamppb.New(time.Unix(g.CreatedAt, 0))
	}
	return out
}

func membershipToProto(m platform.Membership) *pb.Membership {
	out := &pb.Membership{
		Subject:     m.Subject,
		GenealogyId: m.GenealogyID,
		Role:        roleToProto(m.Role),
		GrantedBy:   m.GrantedBy,
	}
	if m.GrantedAt > 0 {
		out.GrantedAt = timestamppb.New(time.Unix(m.GrantedAt, 0))
	}
	return out
}

func banToProto(b platform.Ban) *pb.Ban {
	out := &pb.Ban{
		Subject:     b.Subject,
		GenealogyId: b.GenealogyID,
		Reason:      b.Reason,
		By:          b.By,
	}
	if b.At > 0 {
		out.At = timestamppb.New(time.Unix(b.At, 0))
	}
	return out
}
