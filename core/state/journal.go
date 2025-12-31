// Package state implements the blockchain state management layer for NovaCoin.
package state

import (
	"math/big"
)

// JournalEntry represents a single reversible state change.
type JournalEntry interface {
	// Revert undoes the change.
	Revert(s *StateDB)

	// Dirty returns the address modified by this entry.
	Dirty() *Address
}

// Journal tracks state changes for reversion.
type Journal struct {
	entries []JournalEntry
	// Snapshots store indices into entries for reverting to specific points
	snapshots []int
}

// NewJournal creates a new journal.
func NewJournal() *Journal {
	return &Journal{
		entries:   make([]JournalEntry, 0, 64),
		snapshots: make([]int, 0, 8),
	}
}

// Append adds an entry to the journal.
func (j *Journal) Append(entry JournalEntry) {
	j.entries = append(j.entries, entry)
}

// Snapshot creates a snapshot and returns its ID.
func (j *Journal) Snapshot() int {
	id := len(j.snapshots)
	j.snapshots = append(j.snapshots, len(j.entries))
	return id
}

// RevertToSnapshot reverts all changes made after the snapshot.
func (j *Journal) RevertToSnapshot(s *StateDB, id int) {
	if id >= len(j.snapshots) {
		return
	}

	// Get the journal index for this snapshot
	revertIndex := j.snapshots[id]

	// Revert entries in reverse order
	for i := len(j.entries) - 1; i >= revertIndex; i-- {
		j.entries[i].Revert(s)
	}

	// Truncate journal and snapshots
	j.entries = j.entries[:revertIndex]
	j.snapshots = j.snapshots[:id]
}

// Dirty returns all addresses modified in the journal.
func (j *Journal) Dirty() map[Address]struct{} {
	dirty := make(map[Address]struct{})
	for _, entry := range j.entries {
		if addr := entry.Dirty(); addr != nil {
			dirty[*addr] = struct{}{}
		}
	}
	return dirty
}

// Length returns the number of entries.
func (j *Journal) Length() int {
	return len(j.entries)
}

// Clear clears all entries and snapshots.
func (j *Journal) Clear() {
	j.entries = j.entries[:0]
	j.snapshots = j.snapshots[:0]
}

// === Journal Entry Types ===

// createAccountChange represents account creation.
type createAccountChange struct {
	address Address
}

func (c *createAccountChange) Revert(s *StateDB) {
	delete(s.accounts, c.address)
	delete(s.accountsDirty, c.address)
}

func (c *createAccountChange) Dirty() *Address {
	return &c.address
}

// balanceChange represents a balance modification.
type balanceChange struct {
	address Address
	prev    *big.Int
}

func (c *balanceChange) Revert(s *StateDB) {
	if acc, ok := s.accounts[c.address]; ok {
		acc.Balance = c.prev
	}
}

func (c *balanceChange) Dirty() *Address {
	return &c.address
}

// nonceChange represents a nonce modification.
type nonceChange struct {
	address Address
	prev    uint64
}

func (c *nonceChange) Revert(s *StateDB) {
	if acc, ok := s.accounts[c.address]; ok {
		acc.Nonce = c.prev
	}
}

func (c *nonceChange) Dirty() *Address {
	return &c.address
}

// codeChange represents a code modification.
type codeChange struct {
	address  Address
	prevCode []byte
	prevHash Hash
}

func (c *codeChange) Revert(s *StateDB) {
	if acc, ok := s.accounts[c.address]; ok {
		acc.CodeHash = c.prevHash
		if c.prevCode != nil {
			s.code[c.address] = c.prevCode
		} else {
			delete(s.code, c.address)
		}
	}
}

func (c *codeChange) Dirty() *Address {
	return &c.address
}

// storageChange represents a storage slot modification.
type storageChange struct {
	address Address
	key     Hash
	prev    Hash
}

func (c *storageChange) Revert(s *StateDB) {
	if storage, ok := s.storage[c.address]; ok {
		if c.prev == EmptyHash {
			delete(storage, c.key)
		} else {
			storage[c.key] = c.prev
		}
	}
}

func (c *storageChange) Dirty() *Address {
	return &c.address
}

// suicideChange represents account self-destruction.
type suicideChange struct {
	address     Address
	prev        bool
	prevAccount *Account
	prevCode    []byte
	prevStorage map[Hash]Hash
}

func (c *suicideChange) Revert(s *StateDB) {
	if !c.prev {
		delete(s.suicided, c.address)
	}
	if c.prevAccount != nil {
		s.accounts[c.address] = c.prevAccount
	}
	if c.prevCode != nil {
		s.code[c.address] = c.prevCode
	}
	if c.prevStorage != nil {
		s.storage[c.address] = c.prevStorage
	}
}

func (c *suicideChange) Dirty() *Address {
	return &c.address
}

// touchChange represents an account being touched (for EIP-161 empty account cleanup).
type touchChange struct {
	address Address
}

func (c *touchChange) Revert(s *StateDB) {
	// Touch changes don't need to be reverted
}

func (c *touchChange) Dirty() *Address {
	return &c.address
}

// refundChange represents a gas refund modification.
type refundChange struct {
	prev uint64
}

func (c *refundChange) Revert(s *StateDB) {
	s.refund = c.prev
}

func (c *refundChange) Dirty() *Address {
	return nil
}

// accessListAddAccountChange represents adding an account to access list.
type accessListAddAccountChange struct {
	address Address
}

func (c *accessListAddAccountChange) Revert(s *StateDB) {
	s.accessList.DeleteAddress(c.address)
}

func (c *accessListAddAccountChange) Dirty() *Address {
	return nil
}

// accessListAddSlotChange represents adding a storage slot to access list.
type accessListAddSlotChange struct {
	address Address
	slot    Hash
}

func (c *accessListAddSlotChange) Revert(s *StateDB) {
	s.accessList.DeleteSlot(c.address, c.slot)
}

func (c *accessListAddSlotChange) Dirty() *Address {
	return nil
}

// transientStorageChange represents a transient storage modification (EIP-1153).
type transientStorageChange struct {
	address Address
	key     Hash
	prev    Hash
}

func (c *transientStorageChange) Revert(s *StateDB) {
	s.setTransientState(c.address, c.key, c.prev)
}

func (c *transientStorageChange) Dirty() *Address {
	return nil
}

// AccessList tracks accessed addresses and storage slots (EIP-2929).
type AccessList struct {
	addresses map[Address]int            // Address -> index in slots
	slots     []map[Hash]struct{}        // Per-address slot sets
}

// NewAccessList creates a new access list.
func NewAccessList() *AccessList {
	return &AccessList{
		addresses: make(map[Address]int),
		slots:     make([]map[Hash]struct{}, 0),
	}
}

// Copy creates a deep copy of the access list.
func (al *AccessList) Copy() *AccessList {
	cpy := NewAccessList()
	for addr, idx := range al.addresses {
		cpy.addresses[addr] = len(cpy.slots)
		slotCopy := make(map[Hash]struct{})
		for slot := range al.slots[idx] {
			slotCopy[slot] = struct{}{}
		}
		cpy.slots = append(cpy.slots, slotCopy)
	}
	return cpy
}

// ContainsAddress returns true if the address is in the access list.
func (al *AccessList) ContainsAddress(address Address) bool {
	_, ok := al.addresses[address]
	return ok
}

// Contains checks if address and slot are in the access list.
func (al *AccessList) Contains(address Address, slot Hash) (addressPresent, slotPresent bool) {
	idx, ok := al.addresses[address]
	if !ok {
		return false, false
	}
	if al.slots[idx] == nil {
		return true, false
	}
	_, slotOk := al.slots[idx][slot]
	return true, slotOk
}

// AddAddress adds an address to the access list, returning true if it was new.
func (al *AccessList) AddAddress(address Address) bool {
	if _, ok := al.addresses[address]; ok {
		return false
	}
	al.addresses[address] = len(al.slots)
	al.slots = append(al.slots, nil)
	return true
}

// AddSlot adds a slot to the access list, returning (addressNew, slotNew).
func (al *AccessList) AddSlot(address Address, slot Hash) (addressNew, slotNew bool) {
	idx, ok := al.addresses[address]
	if !ok {
		al.addresses[address] = len(al.slots)
		al.slots = append(al.slots, map[Hash]struct{}{slot: {}})
		return true, true
	}
	if al.slots[idx] == nil {
		al.slots[idx] = make(map[Hash]struct{})
	}
	if _, ok := al.slots[idx][slot]; ok {
		return false, false
	}
	al.slots[idx][slot] = struct{}{}
	return false, true
}

// DeleteAddress removes an address from the access list.
func (al *AccessList) DeleteAddress(address Address) {
	delete(al.addresses, address)
}

// DeleteSlot removes a slot from the access list.
func (al *AccessList) DeleteSlot(address Address, slot Hash) {
	if idx, ok := al.addresses[address]; ok && al.slots[idx] != nil {
		delete(al.slots[idx], slot)
	}
}

// TransientStorage represents transient storage (EIP-1153).
type TransientStorage map[Address]map[Hash]Hash

// NewTransientStorage creates new transient storage.
func NewTransientStorage() TransientStorage {
	return make(TransientStorage)
}

// Set sets a value in transient storage.
func (ts TransientStorage) Set(addr Address, key, value Hash) {
	if _, ok := ts[addr]; !ok {
		ts[addr] = make(map[Hash]Hash)
	}
	ts[addr][key] = value
}

// Get gets a value from transient storage.
func (ts TransientStorage) Get(addr Address, key Hash) Hash {
	if storage, ok := ts[addr]; ok {
		return storage[key]
	}
	return EmptyHash
}

// Copy creates a deep copy.
func (ts TransientStorage) Copy() TransientStorage {
	cpy := make(TransientStorage, len(ts))
	for addr, storage := range ts {
		storageCpy := make(map[Hash]Hash, len(storage))
		for k, v := range storage {
			storageCpy[k] = v
		}
		cpy[addr] = storageCpy
	}
	return cpy
}
