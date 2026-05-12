// SPDX-License-Identifier: AGPL-3.0-or-later

// Package platform owns the global, non-genealogy data the Linea
// server needs to operate: users, genealogies, memberships, bans.
//
// The platform DB is a single Badger instance distinct from the
// per-genealogy stores managed by package tenants. Storing the
// platform metadata separately gives us:
//
//   - one place to answer cross-genealogy questions (e.g. "what
//     genealogies does Alice have access to?") without scanning
//     dozens of per-genealogy databases;
//   - a clean failure boundary: corruption of one genealogy's
//     data does not affect platform metadata or other genealogies.
//
// Records are JSON-encoded for human-debuggability. Keys use a
// short prefix per record type:
//
//	u/<sub>                     -> User JSON
//	g/<id>                      -> Genealogy JSON
//	m/<genealogy_id>/<sub>      -> Membership JSON  (canonical)
//	mr/<sub>/<genealogy_id>     -> Membership JSON  (reverse index for "my genealogies")
//	b/<genealogy_id>/<sub>      -> Ban JSON
package platform
