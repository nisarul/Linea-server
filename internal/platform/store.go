// SPDX-License-Identifier: AGPL-3.0-or-later

package platform

import (
	"encoding/json"
	"errors"
	"fmt"

	badgerdb "github.com/dgraph-io/badger/v4"
)

// Store is a thin Badger wrapper exposing the platform record
// types. It is goroutine-safe.
type Store struct {
	db *badgerdb.DB
}

// Open opens (or creates) a platform DB at path. Pass an empty
// path for an in-memory DB (tests).
func Open(path string) (*Store, error) {
	var opts badgerdb.Options
	if path == "" {
		opts = badgerdb.DefaultOptions("").WithInMemory(true)
	} else {
		opts = badgerdb.DefaultOptions(path)
	}
	opts.Logger = silentLogger{}
	db, err := badgerdb.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("platform: open: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases all resources.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

type silentLogger struct{}

func (silentLogger) Errorf(string, ...any)   {}
func (silentLogger) Warningf(string, ...any) {}
func (silentLogger) Infof(string, ...any)    {}
func (silentLogger) Debugf(string, ...any)   {}

// ---------- key helpers ----------

func userKey(sub string) []byte                  { return []byte("u/" + sub) }
func genealogyKey(id string) []byte              { return []byte("g/" + id) }
func membershipKey(gid, sub string) []byte       { return []byte("m/" + gid + "/" + sub) }
func reverseMembershipKey(sub, gid string) []byte { return []byte("mr/" + sub + "/" + gid) }
func banKey(gid, sub string) []byte              { return []byte("b/" + gid + "/" + sub) }

// ---------- User ----------

// PutUser inserts or replaces a User.
func (s *Store) PutUser(u User) error {
	if u.Subject == "" {
		return errors.New("platform: user.Subject required")
	}
	return s.put(userKey(u.Subject), u)
}

// GetUser fetches a User by subject. Returns ErrNotFound if absent.
func (s *Store) GetUser(sub string) (User, error) {
	var u User
	err := s.get(userKey(sub), &u)
	return u, err
}

// EnsureUser creates the User record if it does not yet exist.
// Returns the resolved User in either case.
func (s *Store) EnsureUser(sub, email string) (User, error) {
	if existing, err := s.GetUser(sub); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	u := User{Subject: sub, Email: email, CreatedAt: Now()}
	if err := s.PutUser(u); err != nil {
		return User{}, err
	}
	return u, nil
}

// ---------- Genealogy ----------

// PutGenealogy inserts or replaces a Genealogy.
func (s *Store) PutGenealogy(g Genealogy) error {
	if g.ID == "" {
		return errors.New("platform: genealogy.ID required")
	}
	if !g.Visibility.IsValid() {
		return errors.New("platform: genealogy.Visibility invalid")
	}
	return s.put(genealogyKey(g.ID), g)
}

// GetGenealogy fetches a Genealogy by id.
func (s *Store) GetGenealogy(id string) (Genealogy, error) {
	var g Genealogy
	err := s.get(genealogyKey(id), &g)
	return g, err
}

// DeleteGenealogy removes the Genealogy record. Per-tenant data
// in <data-dir>/genealogies/<id>/ is NOT touched; the caller
// (typically the Genealogies service) handles disk cleanup with
// a TenantManager.
func (s *Store) DeleteGenealogy(id string) error {
	return s.db.Update(func(tx *badgerdb.Txn) error {
		return tx.Delete(genealogyKey(id))
	})
}

// IterateGenealogies yields every persisted Genealogy. Order is
// adapter-defined.
func (s *Store) IterateGenealogies(yield func(Genealogy) bool) error {
	return s.scanPrefix([]byte("g/"), func(buf []byte) (bool, error) {
		var g Genealogy
		if err := json.Unmarshal(buf, &g); err != nil {
			return false, err
		}
		return yield(g), nil
	})
}

// ---------- Membership ----------

// PutMembership inserts or replaces a Membership and updates the
// reverse index in the same transaction.
func (s *Store) PutMembership(m Membership) error {
	if m.Subject == "" || m.GenealogyID == "" {
		return errors.New("platform: membership requires Subject and GenealogyID")
	}
	if !m.Role.IsValid() {
		return errors.New("platform: membership.Role invalid")
	}
	if m.GrantedAt == 0 {
		m.GrantedAt = Now()
	}
	buf, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *badgerdb.Txn) error {
		if err := tx.Set(membershipKey(m.GenealogyID, m.Subject), buf); err != nil {
			return err
		}
		return tx.Set(reverseMembershipKey(m.Subject, m.GenealogyID), buf)
	})
}

// GetMembership returns the explicit Membership for (sub, gid).
// Returns ErrNotFound if absent (the caller decides whether an
// implicit role from Genealogy.Visibility applies).
func (s *Store) GetMembership(gid, sub string) (Membership, error) {
	var m Membership
	err := s.get(membershipKey(gid, sub), &m)
	return m, err
}

// DeleteMembership removes both index entries atomically.
func (s *Store) DeleteMembership(gid, sub string) error {
	return s.db.Update(func(tx *badgerdb.Txn) error {
		if err := tx.Delete(membershipKey(gid, sub)); err != nil {
			return err
		}
		return tx.Delete(reverseMembershipKey(sub, gid))
	})
}

// IterateMembershipsForGenealogy yields all members of a genealogy.
func (s *Store) IterateMembershipsForGenealogy(gid string, yield func(Membership) bool) error {
	return s.scanPrefix([]byte("m/"+gid+"/"), func(buf []byte) (bool, error) {
		var m Membership
		if err := json.Unmarshal(buf, &m); err != nil {
			return false, err
		}
		return yield(m), nil
	})
}

// IterateMembershipsForUser yields all genealogies a user has explicit access to.
func (s *Store) IterateMembershipsForUser(sub string, yield func(Membership) bool) error {
	return s.scanPrefix([]byte("mr/"+sub+"/"), func(buf []byte) (bool, error) {
		var m Membership
		if err := json.Unmarshal(buf, &m); err != nil {
			return false, err
		}
		return yield(m), nil
	})
}

// ---------- Ban ----------

// PutBan stores a ban record.
func (s *Store) PutBan(b Ban) error {
	if b.Subject == "" || b.GenealogyID == "" {
		return errors.New("platform: ban requires Subject and GenealogyID")
	}
	if b.At == 0 {
		b.At = Now()
	}
	return s.put(banKey(b.GenealogyID, b.Subject), b)
}

// GetBan returns a ban record if present.
func (s *Store) GetBan(gid, sub string) (Ban, error) {
	var b Ban
	err := s.get(banKey(gid, sub), &b)
	return b, err
}

// DeleteBan removes a ban (unbans the user).
func (s *Store) DeleteBan(gid, sub string) error {
	return s.db.Update(func(tx *badgerdb.Txn) error {
		return tx.Delete(banKey(gid, sub))
	})
}

// IsBanned is a convenience for the auth flow.
func (s *Store) IsBanned(gid, sub string) (bool, error) {
	_, err := s.GetBan(gid, sub)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

// ---------- low-level helpers ----------

func (s *Store) put(k []byte, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *badgerdb.Txn) error {
		return tx.Set(k, buf)
	})
}

func (s *Store) get(k []byte, v any) error {
	return s.db.View(func(tx *badgerdb.Txn) error {
		item, err := tx.Get(k)
		if err == badgerdb.ErrKeyNotFound {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return item.Value(func(buf []byte) error { return json.Unmarshal(buf, v) })
	})
}

// scanPrefix invokes decode for every value under prefix.
func (s *Store) scanPrefix(prefix []byte, decode func([]byte) (cont bool, err error)) error {
	return s.db.View(func(tx *badgerdb.Txn) error {
		it := tx.NewIterator(badgerdb.IteratorOptions{
			PrefetchValues: true,
			Prefix:         prefix,
		})
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var cont bool
			err := it.Item().Value(func(buf []byte) error {
				c, e := decode(buf)
				cont = c
				return e
			})
			if err != nil {
				return err
			}
			if !cont {
				return nil
			}
		}
		return nil
	})
}
