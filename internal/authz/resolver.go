// SPDX-License-Identifier: AGPL-3.0-or-later

package authz

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nisarul/Linea-server/internal/platform"
)

// Decision is the resolved authorization outcome for one request.
type Decision struct {
	GenealogyID string
	Role        platform.Role
	// Implicit is true when the role came from the genealogy's
	// Visibility rather than an explicit Membership row.
	Implicit bool
}

type ctxKey struct{}

// WithDecision attaches a Decision to ctx for handler use.
func WithDecision(ctx context.Context, d Decision) context.Context {
	return context.WithValue(ctx, ctxKey{}, d)
}

// DecisionOf returns the Decision attached to ctx, or zero value
// (Role == platform.RoleNone) if none.
func DecisionOf(ctx context.Context) Decision {
	if v, ok := ctx.Value(ctxKey{}).(Decision); ok {
		return v
	}
	return Decision{}
}

// Resolver computes the per-request Decision.
type Resolver struct {
	platform *platform.Store
	cache    *cache
}

// NewResolver constructs a Resolver. The cache TTL defaults to
// 5 seconds; pass 0 to disable caching.
func NewResolver(p *platform.Store, cacheTTL time.Duration) *Resolver {
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Second
	}
	return &Resolver{
		platform: p,
		cache:    newCache(cacheTTL),
	}
}

// ErrNotAllowed indicates the caller has no effective role on the
// target genealogy (or it does not exist; the caller cannot tell
// the difference, by design).
var ErrNotAllowed = errors.New("authz: not allowed")

// Resolve computes a Decision for (subject, genealogyID).
// subject == "" means an unauthenticated request (only Public
// genealogies will succeed, and only with RoleViewer).
func (r *Resolver) Resolve(ctx context.Context, subject, genealogyID string) (Decision, error) {
	if genealogyID == "" {
		return Decision{}, ErrNotAllowed
	}

	if d, ok := r.cache.get(subject, genealogyID); ok {
		return d, nil
	}

	g, err := r.platform.GetGenealogy(genealogyID)
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			return Decision{}, ErrNotAllowed
		}
		return Decision{}, err
	}

	d, err := r.resolveLocked(subject, g)
	if err != nil {
		return Decision{}, err
	}
	r.cache.put(subject, genealogyID, d)
	return d, nil
}

// resolveLocked is the pure decision logic, without caching.
func (r *Resolver) resolveLocked(subject string, g platform.Genealogy) (Decision, error) {
	// 1. Explicit membership beats everything (when authenticated).
	if subject != "" {
		if m, err := r.platform.GetMembership(g.ID, subject); err == nil {
			return Decision{
				GenealogyID: g.ID,
				Role:        m.Role,
				Implicit:    false,
			}, nil
		} else if !errors.Is(err, platform.ErrNotFound) {
			return Decision{}, err
		}
	}

	// 2. Implicit role per Visibility.
	role := implicitRole(g.Visibility, subject != "")
	if role == platform.RoleNone {
		return Decision{}, ErrNotAllowed
	}
	return Decision{GenealogyID: g.ID, Role: role, Implicit: true}, nil
}

// implicitRole encodes the v0.2 visibility -> role table.
func implicitRole(v platform.Visibility, authenticated bool) platform.Role {
	switch v {
	case platform.VisibilityPublic:
		if authenticated {
			return platform.RoleContributor
		}
		return platform.RoleViewer
	case platform.VisibilityUnlisted:
		// URL is unguessable; possession of the link counts as
		// access. But proposal submission still requires login,
		// so unauthenticated users get 401-equivalent here.
		if authenticated {
			return platform.RoleContributor
		}
		return platform.RoleNone
	case platform.VisibilityPrivate:
		return platform.RoleNone
	}
	return platform.RoleNone
}

// ----- TTL cache -----

type cacheKey struct{ sub, gid string }
type cacheEntry struct {
	d   Decision
	exp time.Time
}

type cache struct {
	ttl time.Duration
	mu  sync.RWMutex
	m   map[cacheKey]cacheEntry
}

func newCache(ttl time.Duration) *cache {
	return &cache{ttl: ttl, m: make(map[cacheKey]cacheEntry)}
}

func (c *cache) get(sub, gid string) (Decision, bool) {
	c.mu.RLock()
	e, ok := c.m[cacheKey{sub, gid}]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.exp) {
		return Decision{}, false
	}
	return e.d, true
}

func (c *cache) put(sub, gid string, d Decision) {
	c.mu.Lock()
	c.m[cacheKey{sub, gid}] = cacheEntry{d: d, exp: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// Invalidate clears all cached decisions for the given (sub, gid).
// Pass empty string as a wildcard. This is called after membership
// or visibility changes so cached decisions don't go stale.
func (r *Resolver) Invalidate(sub, gid string) {
	r.cache.mu.Lock()
	defer r.cache.mu.Unlock()
	for k := range r.cache.m {
		if (sub == "" || k.sub == sub) && (gid == "" || k.gid == gid) {
			delete(r.cache.m, k)
		}
	}
}
