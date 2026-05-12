// SPDX-License-Identifier: AGPL-3.0-or-later

package server_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/config"
	"github.com/nisarul/Linea-server/internal/server"
)

// TestServer_v0_2_EndToEnd brings up a real server with auth
// disabled and exercises the v0.2 multi-tenancy surface:
//  1. Server.Version returns spec metadata.
//  2. Genealogies.CreateGenealogy creates a Private genealogy
//     and the caller is recorded as Owner.
//  3. Listing returns it.
//  4. Per-genealogy Proposals.CreateProposal+Submit+Claim+Accept
//     creates a Person inside that genealogy.
//  5. Persons.ListPersons inside the genealogy returns the new Person.
//
// In disabled-auth mode the AuthInterceptor synthesises an
// "anonymous" subject with RoleCurator, so this test simulates
// a single Owner doing everything end-to-end.
func TestServer_v0_2_EndToEnd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	cfg := config.Config{
		GRPCAddr: "127.0.0.1:0",
		HTTPAddr: "127.0.0.1:0",
		DataDir:  dir,
		AuthMode: "disabled",
		LogLevel: "warn",
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	cfg.GRPCAddr = lis.Addr().String()
	require.NoError(t, lis.Close())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := server.New(ctx, cfg, logger, "test")
	require.NoError(t, err)
	defer srv.Close()

	go func() { _ = srv.Run(ctx) }()
	time.Sleep(200 * time.Millisecond)

	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	// --- Server.Version (no auth required)
	v, err := pb.NewServerClient(conn).Version(ctx, &pb.VersionRequest{})
	require.NoError(t, err)
	require.Equal(t, "v1.1.0", v.GetSpecVersion())

	// --- Create a genealogy (default Private). Caller is Owner.
	gens := pb.NewGenealogiesClient(conn)
	create, err := gens.CreateGenealogy(ctx, &pb.CreateGenealogyRequest{
		Name: "Smoke",
	})
	require.NoError(t, err)
	g := create.GetGenealogy()
	require.NotEmpty(t, g.GetId())
	require.Equal(t, pb.Visibility_VISIBILITY_PRIVATE, g.GetVisibility())
	require.Equal(t, pb.GenealogyRole_GENEALOGY_ROLE_OWNER, g.GetMyRole())

	// --- Listing returns it.
	list, err := gens.ListGenealogies(ctx, &pb.ListGenealogiesRequest{})
	require.NoError(t, err)
	require.Len(t, list.GetGenealogies(), 1)

	// --- CreateProposal -> Submit -> Claim -> Accept.
	payload := map[string]any{
		"names": []map[string]any{
			{"text": "Alice", "type": "full", "preferred": true},
		},
	}
	plBuf, _ := json.Marshal(payload)
	props := pb.NewProposalsClient(conn)
	cresp, err := props.CreateProposal(ctx, &pb.CreateProposalRequest{
		GenealogyId: g.GetId(),
		Action:      pb.ProposalAction_PROPOSAL_ACTION_CREATE,
		EntityKind:  pb.EntityKind_ENTITY_KIND_PERSON,
		Payload:     plBuf,
		Reason:      "smoke",
	})
	require.NoError(t, err)
	pid := cresp.GetProposal().GetId()
	for _, fn := range []func() (*pb.Proposal, error){
		func() (*pb.Proposal, error) {
			return props.Submit(ctx, &pb.TransitionRequest{GenealogyId: g.GetId(), Id: pid})
		},
		func() (*pb.Proposal, error) {
			return props.Claim(ctx, &pb.TransitionRequest{GenealogyId: g.GetId(), Id: pid})
		},
		func() (*pb.Proposal, error) {
			return props.Accept(ctx, &pb.TransitionRequest{GenealogyId: g.GetId(), Id: pid})
		},
	} {
		_, err := fn()
		require.NoError(t, err)
	}

	// --- Person was created inside the genealogy.
	persons := pb.NewPersonsClient(conn)
	pl, err := persons.ListPersons(ctx, &pb.ListPersonsRequest{GenealogyId: g.GetId()})
	require.NoError(t, err)
	require.Len(t, pl.GetPersons(), 1)
	require.Equal(t, "Alice", pl.GetPersons()[0].GetNames()[0].GetText())

	// --- Tenant isolation: a different genealogy id with no membership
	// must return NotFound (existence is privileged info).
	_, err = persons.ListPersons(ctx, &pb.ListPersonsRequest{GenealogyId: "00000000-0000-0000-0000-000000000000"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// Either NotFound (genealogy missing) or InvalidArgument is acceptable
	// for a non-existent id; the key is we did NOT see Alice.
	require.Contains(t, []codes.Code{codes.NotFound, codes.InvalidArgument}, st.Code())

	// --- Bulk reject. Submit 3 garbage proposals and reject them in one call.
	var rejectIds []*pb.Id
	for i := 0; i < 3; i++ {
		c, err := props.CreateProposal(ctx, &pb.CreateProposalRequest{
			GenealogyId: g.GetId(),
			Action:      pb.ProposalAction_PROPOSAL_ACTION_CREATE,
			EntityKind:  pb.EntityKind_ENTITY_KIND_PERSON,
			Payload:     plBuf,
			Reason:      "spam",
		})
		require.NoError(t, err)
		_, err = props.Submit(ctx, &pb.TransitionRequest{GenealogyId: g.GetId(), Id: c.GetProposal().GetId()})
		require.NoError(t, err)
		_, err = props.Claim(ctx, &pb.TransitionRequest{GenealogyId: g.GetId(), Id: c.GetProposal().GetId()})
		require.NoError(t, err)
		rejectIds = append(rejectIds, c.GetProposal().GetId())
	}
	br, err := props.BulkReject(ctx, &pb.BulkRejectRequest{
		GenealogyId: g.GetId(),
		Ids:         rejectIds,
		Reason:      "obvious spam",
	})
	require.NoError(t, err)
	require.Len(t, br.GetResults(), 3)
	for _, r := range br.GetResults() {
		require.True(t, r.GetOk(), "result for %s: %s", r.GetId().GetValue(), r.GetError())
	}
}
