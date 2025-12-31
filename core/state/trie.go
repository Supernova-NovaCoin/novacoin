// Package state implements the blockchain state management layer for NovaCoin.
package state

import (
	"bytes"
	"errors"
	"sync"

	"github.com/Supernova-NovaCoin/novacoin/crypto"
)

// NodeType represents the type of a trie node.
type NodeType uint8

const (
	NodeTypeEmpty NodeType = iota
	NodeTypeLeaf
	NodeTypeExtension
	NodeTypeBranch
)

// TrieNode represents a node in the Merkle Patricia Trie.
type TrieNode struct {
	Type     NodeType
	Key      []byte   // For leaf/extension: the path nibbles
	Value    []byte   // For leaf: the value; for branch: optional value at this path
	Children [16]Hash // For branch: child hashes (16 nibble values)
	Child    Hash     // For extension: single child hash

	// Cached values
	hash  Hash
	dirty bool
}

// Trie is a Merkle Patricia Trie implementation.
type Trie struct {
	db   Database
	root Hash

	// Cache of loaded nodes
	cache map[Hash]*TrieNode

	// Track dirty nodes for commit
	dirty map[Hash]*TrieNode

	mu sync.RWMutex
}

// NewTrie creates a new trie with the given root hash.
func NewTrie(db Database, root Hash) *Trie {
	return &Trie{
		db:    db,
		root:  root,
		cache: make(map[Hash]*TrieNode),
		dirty: make(map[Hash]*TrieNode),
	}
}

// NewEmptyTrie creates a new empty trie.
func NewEmptyTrie(db Database) *Trie {
	return NewTrie(db, EmptyRootHash)
}

// Root returns the current root hash.
func (t *Trie) Root() Hash {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.root
}

// Get retrieves a value from the trie.
func (t *Trie) Get(key []byte) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.root == EmptyRootHash {
		return nil, nil
	}

	nibbles := keyToNibbles(key)
	return t.get(t.root, nibbles)
}

func (t *Trie) get(nodeHash Hash, nibbles []byte) ([]byte, error) {
	if nodeHash == EmptyHash {
		return nil, nil
	}

	node, err := t.loadNode(nodeHash)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, nil
	}

	switch node.Type {
	case NodeTypeEmpty:
		return nil, nil

	case NodeTypeLeaf:
		if bytes.Equal(node.Key, nibbles) {
			return node.Value, nil
		}
		return nil, nil

	case NodeTypeExtension:
		if len(nibbles) < len(node.Key) {
			return nil, nil
		}
		if !bytes.Equal(nibbles[:len(node.Key)], node.Key) {
			return nil, nil
		}
		return t.get(node.Child, nibbles[len(node.Key):])

	case NodeTypeBranch:
		if len(nibbles) == 0 {
			return node.Value, nil
		}
		childIndex := nibbles[0]
		return t.get(node.Children[childIndex], nibbles[1:])

	default:
		return nil, ErrInvalidNodeType
	}
}

// Put inserts or updates a key-value pair in the trie.
func (t *Trie) Put(key, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	nibbles := keyToNibbles(key)

	newRoot, err := t.put(t.root, nibbles, value)
	if err != nil {
		return err
	}

	t.root = newRoot
	return nil
}

func (t *Trie) put(nodeHash Hash, nibbles, value []byte) (Hash, error) {
	// Empty trie - create leaf
	if nodeHash == EmptyRootHash || nodeHash == EmptyHash {
		leaf := &TrieNode{
			Type:  NodeTypeLeaf,
			Key:   nibbles,
			Value: value,
			dirty: true,
		}
		return t.saveNode(leaf)
	}

	node, err := t.loadNode(nodeHash)
	if err != nil {
		return EmptyHash, err
	}
	if node == nil {
		// Create new leaf
		leaf := &TrieNode{
			Type:  NodeTypeLeaf,
			Key:   nibbles,
			Value: value,
			dirty: true,
		}
		return t.saveNode(leaf)
	}

	switch node.Type {
	case NodeTypeLeaf:
		return t.putIntoLeaf(node, nibbles, value)

	case NodeTypeExtension:
		return t.putIntoExtension(node, nibbles, value)

	case NodeTypeBranch:
		return t.putIntoBranch(node, nibbles, value)

	default:
		return EmptyHash, ErrInvalidNodeType
	}
}

func (t *Trie) putIntoLeaf(node *TrieNode, nibbles, value []byte) (Hash, error) {
	// Same key - update value
	if bytes.Equal(node.Key, nibbles) {
		newLeaf := &TrieNode{
			Type:  NodeTypeLeaf,
			Key:   nibbles,
			Value: value,
			dirty: true,
		}
		return t.saveNode(newLeaf)
	}

	// Find common prefix
	commonLen := commonPrefixLength(node.Key, nibbles)

	// Create branch node
	branch := &TrieNode{
		Type:  NodeTypeBranch,
		dirty: true,
	}

	// Place existing leaf
	if commonLen < len(node.Key) {
		existingNibble := node.Key[commonLen]
		if commonLen+1 == len(node.Key) {
			// Existing becomes value at branch
			if existingNibble < 16 {
				// Create a leaf for the remaining path
				remainingLeaf := &TrieNode{
					Type:  NodeTypeLeaf,
					Key:   node.Key[commonLen+1:],
					Value: node.Value,
					dirty: true,
				}
				hash, err := t.saveNode(remainingLeaf)
				if err != nil {
					return EmptyHash, err
				}
				branch.Children[existingNibble] = hash
			}
		} else {
			// Create leaf with remaining path
			remainingLeaf := &TrieNode{
				Type:  NodeTypeLeaf,
				Key:   node.Key[commonLen+1:],
				Value: node.Value,
				dirty: true,
			}
			hash, err := t.saveNode(remainingLeaf)
			if err != nil {
				return EmptyHash, err
			}
			branch.Children[existingNibble] = hash
		}
	} else {
		// Existing key is prefix - store value in branch
		branch.Value = node.Value
	}

	// Place new value
	if commonLen < len(nibbles) {
		newNibble := nibbles[commonLen]
		if commonLen+1 == len(nibbles) {
			// New becomes value at branch child
			newLeaf := &TrieNode{
				Type:  NodeTypeLeaf,
				Key:   []byte{},
				Value: value,
				dirty: true,
			}
			hash, err := t.saveNode(newLeaf)
			if err != nil {
				return EmptyHash, err
			}
			branch.Children[newNibble] = hash
		} else {
			// Create leaf with remaining path
			newLeaf := &TrieNode{
				Type:  NodeTypeLeaf,
				Key:   nibbles[commonLen+1:],
				Value: value,
				dirty: true,
			}
			hash, err := t.saveNode(newLeaf)
			if err != nil {
				return EmptyHash, err
			}
			branch.Children[newNibble] = hash
		}
	} else {
		// New key is prefix - store value in branch
		branch.Value = value
	}

	// If there's a common prefix, create extension
	if commonLen > 0 {
		branchHash, err := t.saveNode(branch)
		if err != nil {
			return EmptyHash, err
		}

		extension := &TrieNode{
			Type:  NodeTypeExtension,
			Key:   nibbles[:commonLen],
			Child: branchHash,
			dirty: true,
		}
		return t.saveNode(extension)
	}

	return t.saveNode(branch)
}

func (t *Trie) putIntoExtension(node *TrieNode, nibbles, value []byte) (Hash, error) {
	commonLen := commonPrefixLength(node.Key, nibbles)

	if commonLen == len(node.Key) {
		// Full match - continue into child
		newChild, err := t.put(node.Child, nibbles[commonLen:], value)
		if err != nil {
			return EmptyHash, err
		}

		newExt := &TrieNode{
			Type:  NodeTypeExtension,
			Key:   node.Key,
			Child: newChild,
			dirty: true,
		}
		return t.saveNode(newExt)
	}

	// Partial match - need to split
	branch := &TrieNode{
		Type:  NodeTypeBranch,
		dirty: true,
	}

	// Handle remaining extension
	if commonLen+1 < len(node.Key) {
		// Need a new extension for remaining path
		remainingExt := &TrieNode{
			Type:  NodeTypeExtension,
			Key:   node.Key[commonLen+1:],
			Child: node.Child,
			dirty: true,
		}
		hash, err := t.saveNode(remainingExt)
		if err != nil {
			return EmptyHash, err
		}
		branch.Children[node.Key[commonLen]] = hash
	} else {
		// Extension points directly to child
		branch.Children[node.Key[commonLen]] = node.Child
	}

	// Handle new value
	if commonLen < len(nibbles) {
		newNibble := nibbles[commonLen]
		remainingNibbles := nibbles[commonLen+1:]

		if len(remainingNibbles) == 0 {
			// Value goes at branch
			newLeaf := &TrieNode{
				Type:  NodeTypeLeaf,
				Key:   []byte{},
				Value: value,
				dirty: true,
			}
			hash, err := t.saveNode(newLeaf)
			if err != nil {
				return EmptyHash, err
			}
			branch.Children[newNibble] = hash
		} else {
			newLeaf := &TrieNode{
				Type:  NodeTypeLeaf,
				Key:   remainingNibbles,
				Value: value,
				dirty: true,
			}
			hash, err := t.saveNode(newLeaf)
			if err != nil {
				return EmptyHash, err
			}
			branch.Children[newNibble] = hash
		}
	} else {
		branch.Value = value
	}

	// Create extension for common prefix if needed
	if commonLen > 0 {
		branchHash, err := t.saveNode(branch)
		if err != nil {
			return EmptyHash, err
		}

		extension := &TrieNode{
			Type:  NodeTypeExtension,
			Key:   nibbles[:commonLen],
			Child: branchHash,
			dirty: true,
		}
		return t.saveNode(extension)
	}

	return t.saveNode(branch)
}

func (t *Trie) putIntoBranch(node *TrieNode, nibbles, value []byte) (Hash, error) {
	newBranch := &TrieNode{
		Type:     NodeTypeBranch,
		Value:    node.Value,
		Children: node.Children,
		dirty:    true,
	}

	if len(nibbles) == 0 {
		// Value at this branch
		newBranch.Value = value
		return t.saveNode(newBranch)
	}

	childIndex := nibbles[0]
	newChild, err := t.put(node.Children[childIndex], nibbles[1:], value)
	if err != nil {
		return EmptyHash, err
	}

	newBranch.Children[childIndex] = newChild
	return t.saveNode(newBranch)
}

// Delete removes a key from the trie.
func (t *Trie) Delete(key []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	nibbles := keyToNibbles(key)
	newRoot, _, err := t.delete(t.root, nibbles)
	if err != nil {
		return err
	}

	t.root = newRoot
	return nil
}

func (t *Trie) delete(nodeHash Hash, nibbles []byte) (Hash, bool, error) {
	if nodeHash == EmptyRootHash || nodeHash == EmptyHash {
		return EmptyRootHash, false, nil
	}

	node, err := t.loadNode(nodeHash)
	if err != nil {
		return EmptyHash, false, err
	}
	if node == nil {
		return EmptyRootHash, false, nil
	}

	switch node.Type {
	case NodeTypeLeaf:
		if bytes.Equal(node.Key, nibbles) {
			return EmptyRootHash, true, nil
		}
		return nodeHash, false, nil

	case NodeTypeExtension:
		if len(nibbles) < len(node.Key) || !bytes.Equal(nibbles[:len(node.Key)], node.Key) {
			return nodeHash, false, nil
		}

		newChild, deleted, err := t.delete(node.Child, nibbles[len(node.Key):])
		if err != nil {
			return EmptyHash, false, err
		}
		if !deleted {
			return nodeHash, false, nil
		}

		if newChild == EmptyRootHash {
			return EmptyRootHash, true, nil
		}

		// Check if child should be merged
		childNode, err := t.loadNode(newChild)
		if err != nil {
			return EmptyHash, false, err
		}

		if childNode != nil {
			switch childNode.Type {
			case NodeTypeLeaf:
				// Merge with leaf
				mergedLeaf := &TrieNode{
					Type:  NodeTypeLeaf,
					Key:   append(node.Key, childNode.Key...),
					Value: childNode.Value,
					dirty: true,
				}
				hash, err := t.saveNode(mergedLeaf)
				return hash, true, err

			case NodeTypeExtension:
				// Merge extensions
				mergedExt := &TrieNode{
					Type:  NodeTypeExtension,
					Key:   append(node.Key, childNode.Key...),
					Child: childNode.Child,
					dirty: true,
				}
				hash, err := t.saveNode(mergedExt)
				return hash, true, err
			}
		}

		newExt := &TrieNode{
			Type:  NodeTypeExtension,
			Key:   node.Key,
			Child: newChild,
			dirty: true,
		}
		hash, err := t.saveNode(newExt)
		return hash, true, err

	case NodeTypeBranch:
		if len(nibbles) == 0 {
			// Delete value at branch
			node.Value = nil
		} else {
			childIndex := nibbles[0]
			newChild, deleted, err := t.delete(node.Children[childIndex], nibbles[1:])
			if err != nil {
				return EmptyHash, false, err
			}
			if !deleted {
				return nodeHash, false, nil
			}
			node.Children[childIndex] = newChild
		}

		// Check if branch can be simplified
		return t.simplifyBranch(node)

	default:
		return EmptyHash, false, ErrInvalidNodeType
	}
}

func (t *Trie) simplifyBranch(node *TrieNode) (Hash, bool, error) {
	// Count non-empty children
	nonEmpty := 0
	lastIndex := -1
	for i, child := range node.Children {
		if child != EmptyHash {
			nonEmpty++
			lastIndex = i
		}
	}

	hasValue := len(node.Value) > 0

	if nonEmpty == 0 && !hasValue {
		return EmptyRootHash, true, nil
	}

	if nonEmpty == 1 && !hasValue {
		// Can simplify to extension or leaf
		childNode, err := t.loadNode(node.Children[lastIndex])
		if err != nil {
			return EmptyHash, false, err
		}

		if childNode != nil {
			switch childNode.Type {
			case NodeTypeLeaf:
				mergedLeaf := &TrieNode{
					Type:  NodeTypeLeaf,
					Key:   append([]byte{byte(lastIndex)}, childNode.Key...),
					Value: childNode.Value,
					dirty: true,
				}
				hash, err := t.saveNode(mergedLeaf)
				return hash, true, err

			case NodeTypeExtension:
				mergedExt := &TrieNode{
					Type:  NodeTypeExtension,
					Key:   append([]byte{byte(lastIndex)}, childNode.Key...),
					Child: childNode.Child,
					dirty: true,
				}
				hash, err := t.saveNode(mergedExt)
				return hash, true, err
			}
		}

		// Create extension to child
		ext := &TrieNode{
			Type:  NodeTypeExtension,
			Key:   []byte{byte(lastIndex)},
			Child: node.Children[lastIndex],
			dirty: true,
		}
		hash, err := t.saveNode(ext)
		return hash, true, err
	}

	if nonEmpty == 0 && hasValue {
		// Convert to leaf
		leaf := &TrieNode{
			Type:  NodeTypeLeaf,
			Key:   []byte{},
			Value: node.Value,
			dirty: true,
		}
		hash, err := t.saveNode(leaf)
		return hash, true, err
	}

	// Keep as branch
	newBranch := &TrieNode{
		Type:     NodeTypeBranch,
		Value:    node.Value,
		Children: node.Children,
		dirty:    true,
	}
	hash, err := t.saveNode(newBranch)
	return hash, true, err
}

// Commit writes all dirty nodes to the database.
func (t *Trie) Commit() (Hash, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for hash, node := range t.dirty {
		data := t.serializeNode(node)
		if err := t.db.Put(hash[:], data); err != nil {
			return EmptyHash, err
		}
		t.cache[hash] = node
	}

	t.dirty = make(map[Hash]*TrieNode)
	return t.root, nil
}

// loadNode loads a node from cache or database.
func (t *Trie) loadNode(hash Hash) (*TrieNode, error) {
	if hash == EmptyHash || hash == EmptyRootHash {
		return nil, nil
	}

	// Check dirty nodes first
	if node, ok := t.dirty[hash]; ok {
		return node, nil
	}

	// Check cache
	if node, ok := t.cache[hash]; ok {
		return node, nil
	}

	// Load from database
	data, err := t.db.Get(hash[:])
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	node, err := t.deserializeNode(data)
	if err != nil {
		return nil, err
	}

	t.cache[hash] = node
	return node, nil
}

// saveNode saves a node and returns its hash.
func (t *Trie) saveNode(node *TrieNode) (Hash, error) {
	data := t.serializeNode(node)
	hashBytes := crypto.Hash(data)
	var hash Hash
	copy(hash[:], hashBytes[:])

	node.hash = hash
	t.dirty[hash] = node

	return hash, nil
}

// serializeNode serializes a node for storage.
func (t *Trie) serializeNode(node *TrieNode) []byte {
	switch node.Type {
	case NodeTypeLeaf:
		// Type(1) + KeyLen(2) + Key + Value
		size := 1 + 2 + len(node.Key) + len(node.Value)
		data := make([]byte, size)
		data[0] = byte(NodeTypeLeaf)
		data[1] = byte(len(node.Key) >> 8)
		data[2] = byte(len(node.Key))
		copy(data[3:], node.Key)
		copy(data[3+len(node.Key):], node.Value)
		return data

	case NodeTypeExtension:
		// Type(1) + KeyLen(2) + Key + ChildHash(32)
		size := 1 + 2 + len(node.Key) + 32
		data := make([]byte, size)
		data[0] = byte(NodeTypeExtension)
		data[1] = byte(len(node.Key) >> 8)
		data[2] = byte(len(node.Key))
		copy(data[3:], node.Key)
		copy(data[3+len(node.Key):], node.Child[:])
		return data

	case NodeTypeBranch:
		// Type(1) + Children(16*32) + ValueLen(2) + Value
		size := 1 + 16*32 + 2 + len(node.Value)
		data := make([]byte, size)
		data[0] = byte(NodeTypeBranch)
		for i, child := range node.Children {
			copy(data[1+i*32:], child[:])
		}
		offset := 1 + 16*32
		data[offset] = byte(len(node.Value) >> 8)
		data[offset+1] = byte(len(node.Value))
		copy(data[offset+2:], node.Value)
		return data

	default:
		return []byte{byte(NodeTypeEmpty)}
	}
}

// deserializeNode deserializes a node from storage.
func (t *Trie) deserializeNode(data []byte) (*TrieNode, error) {
	if len(data) == 0 {
		return nil, ErrInvalidNodeData
	}

	nodeType := NodeType(data[0])

	switch nodeType {
	case NodeTypeLeaf:
		if len(data) < 3 {
			return nil, ErrInvalidNodeData
		}
		keyLen := int(data[1])<<8 | int(data[2])
		if len(data) < 3+keyLen {
			return nil, ErrInvalidNodeData
		}
		return &TrieNode{
			Type:  NodeTypeLeaf,
			Key:   data[3 : 3+keyLen],
			Value: data[3+keyLen:],
		}, nil

	case NodeTypeExtension:
		if len(data) < 3 {
			return nil, ErrInvalidNodeData
		}
		keyLen := int(data[1])<<8 | int(data[2])
		if len(data) < 3+keyLen+32 {
			return nil, ErrInvalidNodeData
		}
		var child Hash
		copy(child[:], data[3+keyLen:3+keyLen+32])
		return &TrieNode{
			Type:  NodeTypeExtension,
			Key:   data[3 : 3+keyLen],
			Child: child,
		}, nil

	case NodeTypeBranch:
		if len(data) < 1+16*32+2 {
			return nil, ErrInvalidNodeData
		}
		node := &TrieNode{Type: NodeTypeBranch}
		for i := 0; i < 16; i++ {
			copy(node.Children[i][:], data[1+i*32:1+(i+1)*32])
		}
		offset := 1 + 16*32
		valueLen := int(data[offset])<<8 | int(data[offset+1])
		if len(data) < offset+2+valueLen {
			return nil, ErrInvalidNodeData
		}
		node.Value = data[offset+2 : offset+2+valueLen]
		return node, nil

	default:
		return &TrieNode{Type: NodeTypeEmpty}, nil
	}
}

// Helper functions

// keyToNibbles converts a byte key to nibbles (4-bit values).
func keyToNibbles(key []byte) []byte {
	nibbles := make([]byte, len(key)*2)
	for i, b := range key {
		nibbles[i*2] = b >> 4
		nibbles[i*2+1] = b & 0x0f
	}
	return nibbles
}

// nibblesToKey converts nibbles back to bytes.
func nibblesToKey(nibbles []byte) []byte {
	if len(nibbles)%2 != 0 {
		nibbles = append(nibbles, 0)
	}
	key := make([]byte, len(nibbles)/2)
	for i := 0; i < len(key); i++ {
		key[i] = nibbles[i*2]<<4 | nibbles[i*2+1]
	}
	return key
}

// commonPrefixLength returns the length of the common prefix.
func commonPrefixLength(a, b []byte) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return minLen
}

// Error types
var (
	ErrInvalidNodeType = errors.New("invalid node type")
	ErrInvalidNodeData = errors.New("invalid node data")
)
