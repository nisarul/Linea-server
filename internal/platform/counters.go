// SPDX-License-Identifier: AGPL-3.0-or-later

package platform

import (
	"encoding/binary"
	"errors"
	"sync"

	badgerdb "github.com/dgraph-io/badger/v4"
)

// counterMu serialises counter updates within a single process.
// Badger's MVCC will conflict on hot-write keys; rather than
// retry-with-backoff (which has been observed to lose updates
// under heavy contention) we hold a small global mutex around
// the read-modify-write. This is fine because counter writes are
// not on the latency-sensitive path of any RPC and never exceed
// a handful per second per genealogy in real usage.
var counterMu sync.Mutex

// Counts are the cached counters used to enforce hard quotas
// without scanning per-genealogy stores on every write.
//
// Two counters are maintained:
//
//   c/u/<sub>           uint64 — # private genealogies owned by this user
//   c/g/<gid>/persons   uint64 — # persons in this genealogy
//
// All updates use Badger transactions so increments are atomic
// w.r.t. concurrent calls within a single process.

func userPrivateGenCountKey(sub string) []byte { return []byte("c/u/" + sub) }
func personCountKey(gid string) []byte         { return []byte("c/g/" + gid + "/persons") }

// IncUserPrivateGenCount atomically adds delta to the per-user
// private-genealogy counter and returns the NEW value.
func (s *Store) IncUserPrivateGenCount(sub string, delta int64) (uint64, error) {
	return s.incCounter(userPrivateGenCountKey(sub), delta)
}

// GetUserPrivateGenCount returns the counter without modifying it.
func (s *Store) GetUserPrivateGenCount(sub string) (uint64, error) {
	return s.readCounter(userPrivateGenCountKey(sub))
}

// IncPersonCount atomically adds delta to a genealogy's person
// counter and returns the NEW value.
func (s *Store) IncPersonCount(gid string, delta int64) (uint64, error) {
	return s.incCounter(personCountKey(gid), delta)
}

// GetPersonCount returns a genealogy's person counter.
func (s *Store) GetPersonCount(gid string) (uint64, error) {
	return s.readCounter(personCountKey(gid))
}

// SetPersonCount overwrites the cached counter, used by the
// recount admin operation when on-disk state has drifted.
func (s *Store) SetPersonCount(gid string, n uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, n)
	return s.db.Update(func(tx *badgerdb.Txn) error {
		return tx.Set(personCountKey(gid), buf)
	})
}

// ----- low-level helpers -----

func (s *Store) incCounter(k []byte, delta int64) (uint64, error) {
	counterMu.Lock()
	defer counterMu.Unlock()
	var newVal uint64
	err := s.db.Update(func(tx *badgerdb.Txn) error {
		cur, err := readCounterTx(tx, k)
		if err != nil {
			return err
		}
		// Saturate at zero rather than wrap around on negative deltas
		// — counters should never go below zero in practice.
		if delta < 0 && uint64(-delta) > cur {
			cur = 0
		} else {
			cur = uint64(int64(cur) + delta)
		}
		newVal = cur
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, cur)
		return tx.Set(k, buf)
	})
	return newVal, err
}

func (s *Store) readCounter(k []byte) (uint64, error) {
	var v uint64
	err := s.db.View(func(tx *badgerdb.Txn) error {
		var err error
		v, err = readCounterTx(tx, k)
		return err
	})
	return v, err
}

func readCounterTx(tx *badgerdb.Txn, k []byte) (uint64, error) {
	item, err := tx.Get(k)
	if errors.Is(err, badgerdb.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var v uint64
	err = item.Value(func(buf []byte) error {
		if len(buf) != 8 {
			return errors.New("platform: corrupt counter")
		}
		v = binary.BigEndian.Uint64(buf)
		return nil
	})
	return v, err
}
