// SPDX-License-Identifier: AGPL-3.0-or-later

// Package authz implements the v0.2 authorization flow:
//
//  1. Pull the authenticated identity from ctx (set by AuthInterceptor).
//  2. Resolve the target genealogy id from the request (either the
//     request message field or the URL path via grpc-gateway).
//  3. Look up the user's effective role for that genealogy:
//       - explicit Membership wins
//       - else implicit role per Visibility (Public/Unlisted)
//       - else 403
//  4. Verify the RPC's required role is satisfied.
//  5. Stuff the resolved (tenant, role) into ctx for handlers.
//
// A small TTL cache avoids hammering the platform DB on hot reads.
package authz
