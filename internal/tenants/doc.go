// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tenants owns the per-genealogy Linea-core stores. A
// running server lazily opens each genealogy's Badger directory
// on first reference and caches the handle in a bounded LRU.
// Idle handles are closed on eviction; subsequent references
// reopen them transparently.
//
// The Manager is goroutine-safe; concurrent Get calls for the
// same genealogy share a single open operation via singleflight
// so no genealogy is ever opened twice in parallel.
package tenants
