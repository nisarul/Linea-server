// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
)

const specVersion = "v1.1.0"

// ServerService implements pb.ServerServer. Server.Version is
// genealogy-agnostic — it returns build metadata only.
type ServerService struct {
	pb.UnimplementedServerServer
	ServerVersion string
}

func (s *ServerService) Version(_ context.Context, _ *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{
		SpecVersion:   specVersion,
		ServerVersion: s.ServerVersion,
		// graph_version is left zero in v0.2; per-genealogy graph
		// versions are returned in each entity service's responses.
		GraphVersion: 0,
		// tenant_id remains for backward compatibility with v0.1
		// clients but is no longer meaningful (single-tenant
		// concept removed). Always returns "" in v0.2.
		TenantId: "",
	}, nil
}
