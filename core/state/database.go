// Package state implements the blockchain state management layer for NovaCoin.
package state

import (
	"errors"
	"sync"
)

// Database is the interface for key-value storage.
type Database interface {
	// Get retrieves a value by key.
	Get(key []byte) ([]byte, error)

	// Put stores a key-value pair.
	Put(key, value []byte) error

	// Delete removes a key.
	Delete(key []byte) error

	// Has returns true if the key exists.
	Has(key []byte) (bool, error)

	// NewBatch creates a new batch for atomic writes.
	NewBatch() Batch

	// Close closes the database.
	Close() error
}

// Batch is an interface for batch writes.
type Batch interface {
	// Put adds a key-value pair to the batch.
	Put(key, value []byte) error

	// Delete adds a delete operation to the batch.
	Delete(key []byte) error

	// Write commits the batch to the database.
	Write() error

	// Reset clears the batch.
	Reset()

	// Size returns the number of operations in the batch.
	Size() int
}

// MemoryDatabase is an in-memory implementation of Database.
type MemoryDatabase struct {
	data map[string][]byte
	mu   sync.RWMutex
}

// NewMemoryDatabase creates a new in-memory database.
func NewMemoryDatabase() *MemoryDatabase {
	return &MemoryDatabase{
		data: make(map[string][]byte),
	}
}

// Get retrieves a value by key.
func (db *MemoryDatabase) Get(key []byte) ([]byte, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	value, ok := db.data[string(key)]
	if !ok {
		return nil, ErrNotFound
	}

	// Return a copy to prevent modification
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Put stores a key-value pair.
func (db *MemoryDatabase) Put(key, value []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Store a copy
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	db.data[string(key)] = valueCopy
	return nil
}

// Delete removes a key.
func (db *MemoryDatabase) Delete(key []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	delete(db.data, string(key))
	return nil
}

// Has returns true if the key exists.
func (db *MemoryDatabase) Has(key []byte) (bool, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, ok := db.data[string(key)]
	return ok, nil
}

// NewBatch creates a new batch.
func (db *MemoryDatabase) NewBatch() Batch {
	return &MemoryBatch{
		db:      db,
		puts:   make(map[string][]byte),
		deletes: make(map[string]bool),
	}
}

// Close closes the database.
func (db *MemoryDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.data = nil
	return nil
}

// Size returns the number of entries.
func (db *MemoryDatabase) Size() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.data)
}

// MemoryBatch is an in-memory batch implementation.
type MemoryBatch struct {
	db      *MemoryDatabase
	puts    map[string][]byte
	deletes map[string]bool
	mu      sync.Mutex
}

// Put adds a key-value pair to the batch.
func (b *MemoryBatch) Put(key, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	keyStr := string(key)
	delete(b.deletes, keyStr)

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	b.puts[keyStr] = valueCopy
	return nil
}

// Delete adds a delete operation to the batch.
func (b *MemoryBatch) Delete(key []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	keyStr := string(key)
	delete(b.puts, keyStr)
	b.deletes[keyStr] = true
	return nil
}

// Write commits the batch to the database.
func (b *MemoryBatch) Write() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.db.mu.Lock()
	defer b.db.mu.Unlock()

	for key, value := range b.puts {
		b.db.data[key] = value
	}

	for key := range b.deletes {
		delete(b.db.data, key)
	}

	return nil
}

// Reset clears the batch.
func (b *MemoryBatch) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.puts = make(map[string][]byte)
	b.deletes = make(map[string]bool)
}

// Size returns the number of operations in the batch.
func (b *MemoryBatch) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.puts) + len(b.deletes)
}

// CachingDatabase wraps a database with a cache layer.
type CachingDatabase struct {
	db    Database
	cache map[string][]byte
	dirty map[string][]byte

	maxCacheSize int
	mu           sync.RWMutex
}

// NewCachingDatabase creates a new caching database.
func NewCachingDatabase(db Database, maxCacheSize int) *CachingDatabase {
	if maxCacheSize <= 0 {
		maxCacheSize = 10000
	}
	return &CachingDatabase{
		db:           db,
		cache:        make(map[string][]byte),
		dirty:        make(map[string][]byte),
		maxCacheSize: maxCacheSize,
	}
}

// Get retrieves a value by key.
func (c *CachingDatabase) Get(key []byte) ([]byte, error) {
	c.mu.RLock()
	keyStr := string(key)

	// Check dirty first
	if value, ok := c.dirty[keyStr]; ok {
		c.mu.RUnlock()
		result := make([]byte, len(value))
		copy(result, value)
		return result, nil
	}

	// Check cache
	if value, ok := c.cache[keyStr]; ok {
		c.mu.RUnlock()
		result := make([]byte, len(value))
		copy(result, value)
		return result, nil
	}
	c.mu.RUnlock()

	// Load from database
	value, err := c.db.Get(key)
	if err != nil {
		return nil, err
	}

	// Add to cache
	c.mu.Lock()
	if len(c.cache) < c.maxCacheSize {
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		c.cache[keyStr] = valueCopy
	}
	c.mu.Unlock()

	return value, nil
}

// Put stores a key-value pair (in dirty cache).
func (c *CachingDatabase) Put(key, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	keyStr := string(key)
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	c.dirty[keyStr] = valueCopy
	return nil
}

// Delete removes a key.
func (c *CachingDatabase) Delete(key []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	keyStr := string(key)
	delete(c.cache, keyStr)
	c.dirty[keyStr] = nil // nil marks deletion
	return nil
}

// Has returns true if the key exists.
func (c *CachingDatabase) Has(key []byte) (bool, error) {
	c.mu.RLock()
	keyStr := string(key)

	if value, ok := c.dirty[keyStr]; ok {
		c.mu.RUnlock()
		return value != nil, nil
	}

	if _, ok := c.cache[keyStr]; ok {
		c.mu.RUnlock()
		return true, nil
	}
	c.mu.RUnlock()

	return c.db.Has(key)
}

// NewBatch creates a new batch.
func (c *CachingDatabase) NewBatch() Batch {
	return &CachingBatch{
		cache: c,
		ops:   make([]batchOp, 0),
	}
}

// Flush writes all dirty data to the underlying database.
func (c *CachingDatabase) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	batch := c.db.NewBatch()
	for key, value := range c.dirty {
		if value == nil {
			batch.Delete([]byte(key))
		} else {
			batch.Put([]byte(key), value)
		}
	}

	if err := batch.Write(); err != nil {
		return err
	}

	// Move dirty to cache
	for key, value := range c.dirty {
		if value == nil {
			delete(c.cache, key)
		} else if len(c.cache) < c.maxCacheSize {
			c.cache[key] = value
		}
	}
	c.dirty = make(map[string][]byte)

	return nil
}

// Close closes the database.
func (c *CachingDatabase) Close() error {
	if err := c.Flush(); err != nil {
		return err
	}
	return c.db.Close()
}

// CachingBatch is a batch for CachingDatabase.
type CachingBatch struct {
	cache *CachingDatabase
	ops   []batchOp
	mu    sync.Mutex
}

type batchOp struct {
	key    []byte
	value  []byte
	delete bool
}

// Put adds a key-value pair to the batch.
func (b *CachingBatch) Put(key, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	b.ops = append(b.ops, batchOp{key: keyCopy, value: valueCopy})
	return nil
}

// Delete adds a delete operation to the batch.
func (b *CachingBatch) Delete(key []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	b.ops = append(b.ops, batchOp{key: keyCopy, delete: true})
	return nil
}

// Write commits the batch to the database.
func (b *CachingBatch) Write() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, op := range b.ops {
		if op.delete {
			if err := b.cache.Delete(op.key); err != nil {
				return err
			}
		} else {
			if err := b.cache.Put(op.key, op.value); err != nil {
				return err
			}
		}
	}
	return nil
}

// Reset clears the batch.
func (b *CachingBatch) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ops = b.ops[:0]
}

// Size returns the number of operations in the batch.
func (b *CachingBatch) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.ops)
}

// Prefixed key helpers

// PrefixedDatabase wraps a database with a key prefix.
type PrefixedDatabase struct {
	db     Database
	prefix []byte
}

// NewPrefixedDatabase creates a prefixed database.
func NewPrefixedDatabase(db Database, prefix []byte) *PrefixedDatabase {
	return &PrefixedDatabase{
		db:     db,
		prefix: prefix,
	}
}

func (p *PrefixedDatabase) prefixKey(key []byte) []byte {
	result := make([]byte, len(p.prefix)+len(key))
	copy(result, p.prefix)
	copy(result[len(p.prefix):], key)
	return result
}

// Get retrieves a value by key.
func (p *PrefixedDatabase) Get(key []byte) ([]byte, error) {
	return p.db.Get(p.prefixKey(key))
}

// Put stores a key-value pair.
func (p *PrefixedDatabase) Put(key, value []byte) error {
	return p.db.Put(p.prefixKey(key), value)
}

// Delete removes a key.
func (p *PrefixedDatabase) Delete(key []byte) error {
	return p.db.Delete(p.prefixKey(key))
}

// Has returns true if the key exists.
func (p *PrefixedDatabase) Has(key []byte) (bool, error) {
	return p.db.Has(p.prefixKey(key))
}

// NewBatch creates a new batch.
func (p *PrefixedDatabase) NewBatch() Batch {
	return &PrefixedBatch{
		batch:  p.db.NewBatch(),
		prefix: p.prefix,
	}
}

// Close closes the database.
func (p *PrefixedDatabase) Close() error {
	return p.db.Close()
}

// PrefixedBatch is a batch with key prefixing.
type PrefixedBatch struct {
	batch  Batch
	prefix []byte
}

func (pb *PrefixedBatch) prefixKey(key []byte) []byte {
	result := make([]byte, len(pb.prefix)+len(key))
	copy(result, pb.prefix)
	copy(result[len(pb.prefix):], key)
	return result
}

// Put adds a key-value pair to the batch.
func (pb *PrefixedBatch) Put(key, value []byte) error {
	return pb.batch.Put(pb.prefixKey(key), value)
}

// Delete adds a delete operation to the batch.
func (pb *PrefixedBatch) Delete(key []byte) error {
	return pb.batch.Delete(pb.prefixKey(key))
}

// Write commits the batch.
func (pb *PrefixedBatch) Write() error {
	return pb.batch.Write()
}

// Reset clears the batch.
func (pb *PrefixedBatch) Reset() {
	pb.batch.Reset()
}

// Size returns the number of operations.
func (pb *PrefixedBatch) Size() int {
	return pb.batch.Size()
}

// Key prefixes for different data types
var (
	PrefixAccount     = []byte("a") // Account data
	PrefixCode        = []byte("c") // Contract code
	PrefixStorage     = []byte("s") // Contract storage
	PrefixTrieNode    = []byte("t") // Trie nodes
	PrefixBlockState  = []byte("b") // Block state roots
	PrefixTransaction = []byte("x") // Transaction receipts
)

// Error types
var (
	ErrNotFound = errors.New("not found")
	ErrClosed   = errors.New("database closed")
)
