// Package state implements the blockchain state management layer for NovaCoin.
package state

import (
	"math/big"
	"sync"

	"github.com/Supernova-NovaCoin/novacoin/crypto"
)

// StateDB is the main state database that provides access to accounts,
// contract code, and storage. It supports snapshots for reverting state
// changes during transaction execution.
type StateDB struct {
	db   Database
	trie *Trie

	// Cached account state
	accounts      map[Address]*Account
	accountsDirty map[Address]struct{}

	// Contract code cache
	code      map[Address][]byte
	codeDirty map[Address]struct{}

	// Contract storage cache
	storage      map[Address]map[Hash]Hash
	storageDirty map[Address]map[Hash]struct{}

	// Storage tries (per account)
	storageTries map[Address]*Trie

	// Self-destructed accounts
	suicided map[Address]bool

	// Access list for EIP-2929
	accessList *AccessList

	// Transient storage for EIP-1153
	transientStorage TransientStorage

	// Gas refund counter
	refund uint64

	// Journal for state changes
	journal *Journal

	// Current block/transaction context
	blockNumber uint64
	blockHash   Hash

	mu sync.RWMutex
}

// NewStateDB creates a new state database.
func NewStateDB(db Database, root Hash) (*StateDB, error) {
	trie := NewTrie(db, root)

	return &StateDB{
		db:               db,
		trie:             trie,
		accounts:         make(map[Address]*Account),
		accountsDirty:    make(map[Address]struct{}),
		code:             make(map[Address][]byte),
		codeDirty:        make(map[Address]struct{}),
		storage:          make(map[Address]map[Hash]Hash),
		storageDirty:     make(map[Address]map[Hash]struct{}),
		storageTries:     make(map[Address]*Trie),
		suicided:         make(map[Address]bool),
		accessList:       NewAccessList(),
		transientStorage: NewTransientStorage(),
		journal:          NewJournal(),
	}, nil
}

// Root returns the current state root.
func (s *StateDB) Root() Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.trie.Root()
}

// SetBlockContext sets the current block context.
func (s *StateDB) SetBlockContext(number uint64, hash Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockNumber = number
	s.blockHash = hash
}

// === Account Operations ===

// GetAccount returns the account for the given address.
func (s *StateDB) GetAccount(addr Address) *Account {
	s.mu.RLock()
	if acc, ok := s.accounts[addr]; ok {
		s.mu.RUnlock()
		return acc.Copy()
	}
	s.mu.RUnlock()

	return s.loadAccount(addr)
}

// loadAccount loads an account from the trie.
func (s *StateDB) loadAccount(addr Address) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if acc, ok := s.accounts[addr]; ok {
		return acc.Copy()
	}

	data, err := s.trie.Get(addr[:])
	if err != nil || data == nil {
		return nil
	}

	acc, err := DeserializeAccount(data)
	if err != nil {
		return nil
	}

	s.accounts[addr] = acc
	return acc.Copy()
}

// getOrCreateAccount returns an account, creating it if needed.
func (s *StateDB) getOrCreateAccount(addr Address) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()

	if acc, ok := s.accounts[addr]; ok {
		return acc
	}

	// Try to load from trie
	data, err := s.trie.Get(addr[:])
	if err == nil && data != nil {
		acc, err := DeserializeAccount(data)
		if err == nil {
			s.accounts[addr] = acc
			return acc
		}
	}

	// Create new account
	acc := NewAccount()
	s.accounts[addr] = acc
	s.accountsDirty[addr] = struct{}{}
	s.journal.Append(&createAccountChange{address: addr})
	return acc
}

// CreateAccount creates a new account.
func (s *StateDB) CreateAccount(addr Address) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc := NewAccount()
	s.accounts[addr] = acc
	s.accountsDirty[addr] = struct{}{}
	s.journal.Append(&createAccountChange{address: addr})
}

// Exist returns true if the account exists.
func (s *StateDB) Exist(addr Address) bool {
	acc := s.GetAccount(addr)
	return acc != nil
}

// Empty returns true if the account is empty (EIP-161).
func (s *StateDB) Empty(addr Address) bool {
	acc := s.GetAccount(addr)
	return acc == nil || acc.IsEmpty()
}

// === Balance Operations ===

// GetBalance returns the balance of an account.
func (s *StateDB) GetBalance(addr Address) *big.Int {
	acc := s.GetAccount(addr)
	if acc == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(acc.Balance)
}

// SetBalance sets the balance of an account.
func (s *StateDB) SetBalance(addr Address, balance *big.Int) {
	acc := s.getOrCreateAccount(addr)

	s.mu.Lock()
	defer s.mu.Unlock()

	prev := new(big.Int)
	if acc.Balance != nil {
		prev.Set(acc.Balance)
	}

	s.journal.Append(&balanceChange{address: addr, prev: prev})
	acc.Balance = new(big.Int).Set(balance)
	s.accountsDirty[addr] = struct{}{}
}

// AddBalance adds to the balance of an account.
func (s *StateDB) AddBalance(addr Address, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}

	acc := s.getOrCreateAccount(addr)

	s.mu.Lock()
	defer s.mu.Unlock()

	prev := new(big.Int)
	if acc.Balance != nil {
		prev.Set(acc.Balance)
	}

	s.journal.Append(&balanceChange{address: addr, prev: prev})

	if acc.Balance == nil {
		acc.Balance = new(big.Int)
	}
	acc.Balance.Add(acc.Balance, amount)
	s.accountsDirty[addr] = struct{}{}
}

// SubBalance subtracts from the balance of an account.
func (s *StateDB) SubBalance(addr Address, amount *big.Int) error {
	if amount.Sign() == 0 {
		return nil
	}

	acc := s.getOrCreateAccount(addr)

	s.mu.Lock()
	defer s.mu.Unlock()

	if acc.Balance == nil || acc.Balance.Cmp(amount) < 0 {
		return ErrInsufficientBalance
	}

	prev := new(big.Int).Set(acc.Balance)
	s.journal.Append(&balanceChange{address: addr, prev: prev})

	acc.Balance.Sub(acc.Balance, amount)
	s.accountsDirty[addr] = struct{}{}
	return nil
}

// Transfer transfers balance from one account to another.
func (s *StateDB) Transfer(from, to Address, amount *big.Int) error {
	if amount.Sign() == 0 {
		return nil
	}

	if err := s.SubBalance(from, amount); err != nil {
		return err
	}
	s.AddBalance(to, amount)
	return nil
}

// === Nonce Operations ===

// GetNonce returns the nonce of an account.
func (s *StateDB) GetNonce(addr Address) uint64 {
	acc := s.GetAccount(addr)
	if acc == nil {
		return 0
	}
	return acc.Nonce
}

// SetNonce sets the nonce of an account.
func (s *StateDB) SetNonce(addr Address, nonce uint64) {
	acc := s.getOrCreateAccount(addr)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.journal.Append(&nonceChange{address: addr, prev: acc.Nonce})
	acc.Nonce = nonce
	s.accountsDirty[addr] = struct{}{}
}

// IncrementNonce increments the nonce of an account.
func (s *StateDB) IncrementNonce(addr Address) error {
	acc := s.getOrCreateAccount(addr)

	s.mu.Lock()
	defer s.mu.Unlock()

	if acc.Nonce == ^uint64(0) {
		return ErrNonceOverflow
	}

	s.journal.Append(&nonceChange{address: addr, prev: acc.Nonce})
	acc.Nonce++
	s.accountsDirty[addr] = struct{}{}
	return nil
}

// === Code Operations ===

// GetCode returns the code of an account.
func (s *StateDB) GetCode(addr Address) []byte {
	s.mu.RLock()
	if code, ok := s.code[addr]; ok {
		s.mu.RUnlock()
		result := make([]byte, len(code))
		copy(result, code)
		return result
	}
	s.mu.RUnlock()

	acc := s.GetAccount(addr)
	if acc == nil || acc.CodeHash == HashFromBytes(EmptyCodeHash[:]) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Load from database
	code, err := s.db.Get(append(PrefixCode, acc.CodeHash[:]...))
	if err != nil {
		return nil
	}

	s.code[addr] = code
	return code
}

// GetCodeHash returns the code hash of an account.
func (s *StateDB) GetCodeHash(addr Address) Hash {
	acc := s.GetAccount(addr)
	if acc == nil {
		return HashFromBytes(EmptyCodeHash[:])
	}
	return acc.CodeHash
}

// GetCodeSize returns the size of the code.
func (s *StateDB) GetCodeSize(addr Address) int {
	code := s.GetCode(addr)
	return len(code)
}

// SetCode sets the code of an account.
func (s *StateDB) SetCode(addr Address, code []byte) {
	acc := s.getOrCreateAccount(addr)

	s.mu.Lock()
	defer s.mu.Unlock()

	prevCode := s.code[addr]
	prevHash := acc.CodeHash

	s.journal.Append(&codeChange{
		address:  addr,
		prevCode: prevCode,
		prevHash: prevHash,
	})

	codeHash := crypto.Hash(code)
	acc.CodeHash = HashFromBytes(codeHash[:])

	codeCopy := make([]byte, len(code))
	copy(codeCopy, code)
	s.code[addr] = codeCopy
	s.codeDirty[addr] = struct{}{}
	s.accountsDirty[addr] = struct{}{}
}

// === Storage Operations ===

// GetState returns a storage value.
func (s *StateDB) GetState(addr Address, key Hash) Hash {
	s.mu.RLock()
	if storage, ok := s.storage[addr]; ok {
		if value, ok := storage[key]; ok {
			s.mu.RUnlock()
			return value
		}
	}
	s.mu.RUnlock()

	return s.loadStorageValue(addr, key)
}

// loadStorageValue loads a storage value from the storage trie.
func (s *StateDB) loadStorageValue(addr Address, key Hash) Hash {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if storage, ok := s.storage[addr]; ok {
		if value, ok := storage[key]; ok {
			return value
		}
	}

	// Get storage trie
	trie, err := s.getStorageTrie(addr)
	if err != nil || trie == nil {
		return EmptyHash
	}

	// Load value
	data, err := trie.Get(key[:])
	if err != nil || len(data) == 0 {
		return EmptyHash
	}

	var value Hash
	copy(value[:], data)

	// Cache
	if s.storage[addr] == nil {
		s.storage[addr] = make(map[Hash]Hash)
	}
	s.storage[addr][key] = value

	return value
}

// getStorageTrie returns the storage trie for an account.
func (s *StateDB) getStorageTrie(addr Address) (*Trie, error) {
	if trie, ok := s.storageTries[addr]; ok {
		return trie, nil
	}

	acc := s.accounts[addr]
	if acc == nil {
		return nil, nil
	}

	trie := NewTrie(s.db, acc.StorageRoot)
	s.storageTries[addr] = trie
	return trie, nil
}

// SetState sets a storage value.
func (s *StateDB) SetState(addr Address, key, value Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure account exists
	if _, ok := s.accounts[addr]; !ok {
		s.mu.Unlock()
		s.getOrCreateAccount(addr)
		s.mu.Lock()
	}

	// Get previous value
	prev := EmptyHash
	if storage, ok := s.storage[addr]; ok {
		if v, ok := storage[key]; ok {
			prev = v
		}
	}

	// Journal the change
	s.journal.Append(&storageChange{address: addr, key: key, prev: prev})

	// Update cache
	if s.storage[addr] == nil {
		s.storage[addr] = make(map[Hash]Hash)
	}
	if s.storageDirty[addr] == nil {
		s.storageDirty[addr] = make(map[Hash]struct{})
	}

	s.storage[addr][key] = value
	s.storageDirty[addr][key] = struct{}{}
	s.accountsDirty[addr] = struct{}{}
}

// GetCommittedState returns the committed storage value (before current tx).
func (s *StateDB) GetCommittedState(addr Address, key Hash) Hash {
	// For now, just return the same as GetState
	// In a full implementation, this would check the original trie
	return s.GetState(addr, key)
}

// === Transient Storage (EIP-1153) ===

// GetTransientState gets a transient storage value.
func (s *StateDB) GetTransientState(addr Address, key Hash) Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.transientStorage.Get(addr, key)
}

// SetTransientState sets a transient storage value.
func (s *StateDB) SetTransientState(addr Address, key, value Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.transientStorage.Get(addr, key)
	s.journal.Append(&transientStorageChange{
		address: addr,
		key:     key,
		prev:    prev,
	})

	s.transientStorage.Set(addr, key, value)
}

// setTransientState is the internal version without journaling.
func (s *StateDB) setTransientState(addr Address, key, value Hash) {
	s.transientStorage.Set(addr, key, value)
}

// === Suicide / Self-destruct ===

// Suicide marks an account for deletion.
func (s *StateDB) Suicide(addr Address) bool {
	acc := s.GetAccount(addr)
	if acc == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	prevSuicide := s.suicided[addr]

	s.journal.Append(&suicideChange{
		address:     addr,
		prev:        prevSuicide,
		prevAccount: acc.Copy(),
		prevCode:    s.code[addr],
		prevStorage: s.copyStorage(addr),
	})

	s.suicided[addr] = true

	// Zero balance
	if cachedAcc, ok := s.accounts[addr]; ok {
		cachedAcc.Balance = new(big.Int)
		s.accountsDirty[addr] = struct{}{}
	}

	return true
}

// HasSuicided returns true if the account is marked for suicide.
func (s *StateDB) HasSuicided(addr Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.suicided[addr]
}

func (s *StateDB) copyStorage(addr Address) map[Hash]Hash {
	storage := s.storage[addr]
	if storage == nil {
		return nil
	}
	cpy := make(map[Hash]Hash, len(storage))
	for k, v := range storage {
		cpy[k] = v
	}
	return cpy
}

// === Access List (EIP-2929) ===

// AddAddressToAccessList adds an address to the access list.
func (s *StateDB) AddAddressToAccessList(addr Address) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessList.AddAddress(addr) {
		s.journal.Append(&accessListAddAccountChange{address: addr})
	}
}

// AddSlotToAccessList adds an address and slot to the access list.
func (s *StateDB) AddSlotToAccessList(addr Address, slot Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	addrNew, slotNew := s.accessList.AddSlot(addr, slot)
	if addrNew {
		s.journal.Append(&accessListAddAccountChange{address: addr})
	}
	if slotNew {
		s.journal.Append(&accessListAddSlotChange{address: addr, slot: slot})
	}
}

// AddressInAccessList returns true if the address is in the access list.
func (s *StateDB) AddressInAccessList(addr Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accessList.ContainsAddress(addr)
}

// SlotInAccessList returns if address and slot are in access list.
func (s *StateDB) SlotInAccessList(addr Address, slot Hash) (addressOk, slotOk bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accessList.Contains(addr, slot)
}

// PrepareAccessList prepares the access list for a transaction.
func (s *StateDB) PrepareAccessList(sender Address, dst *Address, precompiles []Address, txAccesses []struct{ Address Address; Slots []Hash }) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.accessList = NewAccessList()
	s.accessList.AddAddress(sender)

	if dst != nil {
		s.accessList.AddAddress(*dst)
	}

	for _, addr := range precompiles {
		s.accessList.AddAddress(addr)
	}

	for _, access := range txAccesses {
		s.accessList.AddAddress(access.Address)
		for _, slot := range access.Slots {
			s.accessList.AddSlot(access.Address, slot)
		}
	}
}

// === Refund ===

// AddRefund adds to the refund counter.
func (s *StateDB) AddRefund(gas uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.journal.Append(&refundChange{prev: s.refund})
	s.refund += gas
}

// SubRefund subtracts from the refund counter.
func (s *StateDB) SubRefund(gas uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.journal.Append(&refundChange{prev: s.refund})
	if gas > s.refund {
		s.refund = 0
	} else {
		s.refund -= gas
	}
}

// GetRefund returns the current refund counter.
func (s *StateDB) GetRefund() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.refund
}

// === Snapshots ===

// Snapshot creates a snapshot of the current state.
func (s *StateDB) Snapshot() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.journal.Snapshot()
}

// RevertToSnapshot reverts to a previous snapshot.
func (s *StateDB) RevertToSnapshot(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.journal.RevertToSnapshot(s, id)
}

// === Commit ===

// Commit commits all changes to the database and returns the new state root.
func (s *StateDB) Commit() (Hash, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update account state in trie
	for addr := range s.accountsDirty {
		acc := s.accounts[addr]
		if acc == nil || (s.suicided[addr] && acc.IsEmpty()) {
			// Delete account
			if err := s.trie.Delete(addr[:]); err != nil {
				return EmptyHash, err
			}
			continue
		}

		// Update storage root if needed
		if storageDirty := s.storageDirty[addr]; len(storageDirty) > 0 {
			trie, err := s.getStorageTrie(addr)
			if err != nil {
				return EmptyHash, err
			}
			if trie == nil {
				trie = NewEmptyTrie(s.db)
				s.storageTries[addr] = trie
			}

			for key := range storageDirty {
				value := s.storage[addr][key]
				if value == EmptyHash {
					if err := trie.Delete(key[:]); err != nil {
						return EmptyHash, err
					}
				} else {
					if err := trie.Put(key[:], value[:]); err != nil {
						return EmptyHash, err
					}
				}
			}

			root, err := trie.Commit()
			if err != nil {
				return EmptyHash, err
			}
			acc.StorageRoot = root
		}

		// Serialize and store account
		data := acc.Serialize()
		if err := s.trie.Put(addr[:], data); err != nil {
			return EmptyHash, err
		}
	}

	// Store code
	for addr := range s.codeDirty {
		code := s.code[addr]
		if len(code) > 0 {
			codeHash := crypto.Hash(code)
			if err := s.db.Put(append(PrefixCode, codeHash[:]...), code); err != nil {
				return EmptyHash, err
			}
		}
	}

	// Commit main trie
	root, err := s.trie.Commit()
	if err != nil {
		return EmptyHash, err
	}

	// Clear dirty flags
	s.accountsDirty = make(map[Address]struct{})
	s.codeDirty = make(map[Address]struct{})
	s.storageDirty = make(map[Address]map[Hash]struct{})

	// Clear suicided accounts
	for addr := range s.suicided {
		delete(s.accounts, addr)
		delete(s.code, addr)
		delete(s.storage, addr)
		delete(s.storageTries, addr)
	}
	s.suicided = make(map[Address]bool)

	// Clear transient storage
	s.transientStorage = NewTransientStorage()

	// Clear journal
	s.journal.Clear()
	s.refund = 0

	return root, nil
}

// Copy creates a deep copy of the state.
func (s *StateDB) Copy() *StateDB {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cpy := &StateDB{
		db:               s.db,
		trie:             NewTrie(s.db, s.trie.Root()),
		accounts:         make(map[Address]*Account),
		accountsDirty:    make(map[Address]struct{}),
		code:             make(map[Address][]byte),
		codeDirty:        make(map[Address]struct{}),
		storage:          make(map[Address]map[Hash]Hash),
		storageDirty:     make(map[Address]map[Hash]struct{}),
		storageTries:     make(map[Address]*Trie),
		suicided:         make(map[Address]bool),
		accessList:       s.accessList.Copy(),
		transientStorage: s.transientStorage.Copy(),
		refund:           s.refund,
		journal:          NewJournal(),
		blockNumber:      s.blockNumber,
		blockHash:        s.blockHash,
	}

	for addr, acc := range s.accounts {
		cpy.accounts[addr] = acc.Copy()
	}
	for addr := range s.accountsDirty {
		cpy.accountsDirty[addr] = struct{}{}
	}
	for addr, code := range s.code {
		codeCopy := make([]byte, len(code))
		copy(codeCopy, code)
		cpy.code[addr] = codeCopy
	}
	for addr := range s.codeDirty {
		cpy.codeDirty[addr] = struct{}{}
	}
	for addr, storage := range s.storage {
		cpy.storage[addr] = make(map[Hash]Hash)
		for k, v := range storage {
			cpy.storage[addr][k] = v
		}
	}
	for addr, dirty := range s.storageDirty {
		cpy.storageDirty[addr] = make(map[Hash]struct{})
		for k := range dirty {
			cpy.storageDirty[addr][k] = struct{}{}
		}
	}
	for addr := range s.suicided {
		cpy.suicided[addr] = true
	}

	return cpy
}

// Finalise finalises the state, clearing the journal.
func (s *StateDB) Finalise(deleteEmptyObjects bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if deleteEmptyObjects {
		for addr := range s.journal.Dirty() {
			if acc, ok := s.accounts[addr]; ok && acc.IsEmpty() {
				s.suicided[addr] = true
			}
		}
	}

	s.journal.Clear()
	s.refund = 0
}

// IntermediateRoot computes the current state root.
func (s *StateDB) IntermediateRoot(deleteEmptyObjects bool) Hash {
	s.Finalise(deleteEmptyObjects)

	// For now, return the committed root
	// In a full implementation, this would compute without committing
	return s.trie.Root()
}
