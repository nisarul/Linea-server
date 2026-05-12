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
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/config"
	"github.com/nisarul/Linea-server/internal/server"
)

// TestServer_EndToEnd brings up a real server with auth disabled
// and a fresh data directory, then drives a complete proposal
// lifecycle over gRPC.
func TestServer_EndToEnd(t *testing.T) {
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

	// Pre-bind the gRPC port so we know the address to dial.
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
	// Tiny delay for listeners to bind. Not great, but the test
	// doesn't run long enough for this to matter.
	time.Sleep(200 * time.Millisecond)

	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	versionClient := pb.NewServerClient(conn)
	v, err := versionClient.Version(ctx, &pb.VersionRequest{})
	require.NoError(t, err)
	require.Equal(t, "v1.1.0", v.GetSpecVersion())
	require.Equal(t, "default", v.GetTenantId())

	// Create-Person proposal end-to-end.
	payload := map[string]any{
		"names": []map[string]any{
			{"text": "Alice", "type": "full", "preferred": true},
		},
	}
	plBuf, _ := json.Marshal(payload)
	props := pb.NewProposalsClient(conn)
	create, err := props.CreateProposal(ctx, &pb.CreateProposalRequest{
		Action:     pb.ProposalAction_PROPOSAL_ACTION_CREATE,
		EntityKind: pb.EntityKind_ENTITY_KIND_PERSON,
		Payload:    plBuf,
		Reason:     "smoke",
	})
	require.NoError(t, err)
	id := create.GetProposal().GetId()

	for _, fn := range []func() (*pb.Proposal, error){
		func() (*pb.Proposal, error) { return props.Submit(ctx, &pb.TransitionRequest{Id: id}) },
		func() (*pb.Proposal, error) { return props.Claim(ctx, &pb.TransitionRequest{Id: id}) },
		func() (*pb.Proposal, error) { return props.Accept(ctx, &pb.TransitionRequest{Id: id}) },
	} {
		_, err := fn()
		require.NoError(t, err)
	}

	persons := pb.NewPersonsClient(conn)
	list, err := persons.ListPersons(ctx, &pb.ListPersonsRequest{})
	require.NoError(t, err)
	require.Len(t, list.GetPersons(), 1)
	require.Equal(t, "Alice", list.GetPersons()[0].GetNames()[0].GetText())
}
