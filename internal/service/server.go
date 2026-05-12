// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"context"

	"github.com/nisarul/Linea-core/store"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/tenancy"
)

const specVersion = "v1.1.0"

// ServerService implements pb.ServerServer.
type ServerService struct {
	pb.UnimplementedServerServer
	Store         store.Store
	ServerVersion string
}

// Version returns metadata about the running server.
func (s *ServerService) Version(ctx context.Context, _ *pb.VersionRequest) (*pb.VersionResponse, error) {
	tenant := tenancy.TenantOf(ctx)
	v, err := s.Store.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.VersionResponse{
		SpecVersion:   specVersion,
		ServerVersion: s.ServerVersion,
		GraphVersion:  uint64(v),
		TenantId:      string(tenant),
	}, nil
}
