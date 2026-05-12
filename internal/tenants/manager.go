// SPDX-License-Identifier: AGPL-3.0-or-later

package tenants

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nisarul/Linea-core/store"
	"github.com/nisarul/Linea-core/store/badger"
)

// ErrShutdown is returned by Get after Close has been called.
var ErrShutdown = errors.New("tenants: manager is shut down")

// Config configures a Manager.
type Config struct {
	// Root is the parent directory where genealogy databases live.
	// Each genealogy uses Root/<id>/.
	Root string
	// MaxOpen caps the number of open Badger handles. Older
	// handles are closed on eviction. Must be >= 1.
	MaxOpen int
}

// Manager owns the LRU cache of open per-genealogy stores.
type Manager struct {
	cfg     Config
	mu      sync.Mutex
	cache   *list.List // front == most recently used; values are *entry
	index   map[string]*list.Element
	loading map[string]*loadOp
	closed  bool
}

type entry struct {
	id    string
	store *badger.Store
}

// loadOp coalesces concurrent Open calls for the same genealogy
// so we never have two opens in flight for the same id.
type loadOp struct {
	done chan struct{}
	st   *badger.Store
	err  error
}

// New constructs a Manager. The root directory is created lazily
// when individual genealogies are opened.
func New(cfg Config) (*Manager, error) {
	if cfg.Root == "" {
		return nil, errors.New("tenants: Config.Root is required")
	}
	if cfg.MaxOpen < 1 {
		return nil, errors.New("tenants: Config.MaxOpen must be >= 1")
	}
	return &Manager{
		cfg:     cfg,
		cache:   list.New(),
		index:   make(map[string]*list.Element),
		loading: make(map[string]*loadOp),
	}, nil
}

// Get returns the store for the given genealogy id, opening it
// if necessary. Concurrent calls for the same id share a single
// open operation. The returned store remains valid for the life
// of the Manager (or until evicted from the cache and closed —
// callers SHOULD NOT cache the pointer beyond the request).
func (m *Manager) Get(_ context.Context, id string) (store.Store, error) {
	if id == "" {
		return nil, errors.New("tenants: id is required")
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrShutdown
	}
	if elem, ok := m.index[id]; ok {
		m.cache.MoveToFront(elem)
		m.mu.Unlock()
		return elem.Value.(*entry).store, nil
	}
	if op, ok := m.loading[id]; ok {
		m.mu.Unlock()
		<-op.done
		return op.st, op.err
	}
	op := &loadOp{done: make(chan struct{})}
	m.loading[id] = op
	m.mu.Unlock()

	st, err := m.openLocked(id)

	m.mu.Lock()
	op.st = st
	op.err = err
	close(op.done)
	delete(m.loading, id)
	if err == nil {
		m.installLocked(id, st)
	}
	m.mu.Unlock()

	return st, err
}

// openLocked is the non-cached open path. It runs WITHOUT the
// Manager mutex held so the actual disk I/O does not serialise
// across Get calls for different ids.
func (m *Manager) openLocked(id string) (*badger.Store, error) {
	dir := filepath.Join(m.cfg.Root, id)
	st, err := badger.Open(dir, badger.Silent())
	if err != nil {
		return nil, fmt.Errorf("tenants: open %s: %w", id, err)
	}
	return st, nil
}

// installLocked is called with m.mu held; it places a freshly
// opened store into the LRU and evicts the oldest if necessary.
func (m *Manager) installLocked(id string, st *badger.Store) {
	if elem, ok := m.index[id]; ok {
		// Lost a race; keep the existing handle, close the new one.
		m.cache.MoveToFront(elem)
		_ = st.Close()
		return
	}
	e := &entry{id: id, store: st}
	elem := m.cache.PushFront(e)
	m.index[id] = elem
	m.evictLocked()
}

// evictLocked closes the LRU tail until size <= MaxOpen.
func (m *Manager) evictLocked() {
	for m.cache.Len() > m.cfg.MaxOpen {
		tail := m.cache.Back()
		if tail == nil {
			return
		}
		e := tail.Value.(*entry)
		m.cache.Remove(tail)
		delete(m.index, e.id)
		_ = e.store.Close()
	}
}

// Drop closes a single genealogy's store and removes it from the
// cache. Used after deleting or archiving a genealogy. It is safe
// to call when the id is not currently cached.
func (m *Manager) Drop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	elem, ok := m.index[id]
	if !ok {
		return nil
	}
	e := elem.Value.(*entry)
	m.cache.Remove(elem)
	delete(m.index, id)
	return e.store.Close()
}

// RemoveOnDisk deletes the on-disk Badger directory for the given
// genealogy. The store MUST be closed first (call Drop). Safe to
// call when the directory does not exist.
func (m *Manager) RemoveOnDisk(id string) error {
	dir := filepath.Join(m.cfg.Root, id)
	return os.RemoveAll(dir)
}

// Close releases all cached stores. Subsequent Get calls return ErrShutdown.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	var firstErr error
	for elem := m.cache.Front(); elem != nil; elem = elem.Next() {
		if err := elem.Value.(*entry).store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.cache.Init()
	m.index = nil
	return firstErr
}

// Len reports the current number of cached open handles. Useful
// for tests and metrics.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cache.Len()
}
