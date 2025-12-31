// Package evm implements the Ethereum Virtual Machine for NovaCoin.
package evm

// OpCode represents an EVM opcode.
type OpCode byte

// === Stop and Arithmetic Operations (0x00 - 0x0f) ===
const (
	STOP       OpCode = 0x00 // Halts execution
	ADD        OpCode = 0x01 // Addition
	MUL        OpCode = 0x02 // Multiplication
	SUB        OpCode = 0x03 // Subtraction
	DIV        OpCode = 0x04 // Integer division
	SDIV       OpCode = 0x05 // Signed integer division
	MOD        OpCode = 0x06 // Modulo remainder
	SMOD       OpCode = 0x07 // Signed modulo remainder
	ADDMOD     OpCode = 0x08 // (a + b) % N
	MULMOD     OpCode = 0x09 // (a * b) % N
	EXP        OpCode = 0x0a // Exponential
	SIGNEXTEND OpCode = 0x0b // Sign extend
)

// === Comparison and Bitwise Logic Operations (0x10 - 0x1f) ===
const (
	LT     OpCode = 0x10 // Less-than comparison
	GT     OpCode = 0x11 // Greater-than comparison
	SLT    OpCode = 0x12 // Signed less-than
	SGT    OpCode = 0x13 // Signed greater-than
	EQ     OpCode = 0x14 // Equality comparison
	ISZERO OpCode = 0x15 // Is zero
	AND    OpCode = 0x16 // Bitwise AND
	OR     OpCode = 0x17 // Bitwise OR
	XOR    OpCode = 0x18 // Bitwise XOR
	NOT    OpCode = 0x19 // Bitwise NOT
	BYTE   OpCode = 0x1a // Retrieve single byte
	SHL    OpCode = 0x1b // Shift left (EIP-145)
	SHR    OpCode = 0x1c // Logical shift right (EIP-145)
	SAR    OpCode = 0x1d // Arithmetic shift right (EIP-145)
)

// === Keccak256 (0x20) ===
const (
	KECCAK256 OpCode = 0x20 // Compute Keccak-256 hash
)

// === Environmental Information (0x30 - 0x3f) ===
const (
	ADDRESS        OpCode = 0x30 // Get address of current contract
	BALANCE        OpCode = 0x31 // Get balance of account
	ORIGIN         OpCode = 0x32 // Get original caller (tx.origin)
	CALLER         OpCode = 0x33 // Get caller address (msg.sender)
	CALLVALUE      OpCode = 0x34 // Get call value (msg.value)
	CALLDATALOAD   OpCode = 0x35 // Get input data
	CALLDATASIZE   OpCode = 0x36 // Get size of input data
	CALLDATACOPY   OpCode = 0x37 // Copy input data to memory
	CODESIZE       OpCode = 0x38 // Get size of code
	CODECOPY       OpCode = 0x39 // Copy code to memory
	GASPRICE       OpCode = 0x3a // Get price of gas
	EXTCODESIZE    OpCode = 0x3b // Get size of external code
	EXTCODECOPY    OpCode = 0x3c // Copy external code to memory
	RETURNDATASIZE OpCode = 0x3d // Size of return data (EIP-211)
	RETURNDATACOPY OpCode = 0x3e // Copy return data to memory (EIP-211)
	EXTCODEHASH    OpCode = 0x3f // Get hash of external code (EIP-1052)
)

// === Block Information (0x40 - 0x4f) ===
const (
	BLOCKHASH   OpCode = 0x40 // Get hash of block
	COINBASE    OpCode = 0x41 // Get block's coinbase address
	TIMESTAMP   OpCode = 0x42 // Get block's timestamp
	NUMBER      OpCode = 0x43 // Get block's number
	PREVRANDAO  OpCode = 0x44 // Get previous block's RANDAO (EIP-4399, replaces DIFFICULTY)
	GASLIMIT    OpCode = 0x45 // Get block's gas limit
	CHAINID     OpCode = 0x46 // Get chain ID (EIP-1344)
	SELFBALANCE OpCode = 0x47 // Get balance of current contract (EIP-1884)
	BASEFEE     OpCode = 0x48 // Get block's base fee (EIP-3198)
	BLOBHASH    OpCode = 0x49 // Get versioned blob hash (EIP-4844)
	BLOBBASEFEE OpCode = 0x4a // Get blob base fee (EIP-7516)
)

// === Stack, Memory, Storage and Flow Operations (0x50 - 0x5f) ===
const (
	POP      OpCode = 0x50 // Remove item from stack
	MLOAD    OpCode = 0x51 // Load word from memory
	MSTORE   OpCode = 0x52 // Save word to memory
	MSTORE8  OpCode = 0x53 // Save byte to memory
	SLOAD    OpCode = 0x54 // Load word from storage
	SSTORE   OpCode = 0x55 // Save word to storage
	JUMP     OpCode = 0x56 // Alter program counter
	JUMPI    OpCode = 0x57 // Conditionally alter program counter
	PC       OpCode = 0x58 // Get program counter
	MSIZE    OpCode = 0x59 // Get size of active memory
	GAS      OpCode = 0x5a // Get available gas
	JUMPDEST OpCode = 0x5b // Mark valid jump destination
	TLOAD    OpCode = 0x5c // Transient storage load (EIP-1153)
	TSTORE   OpCode = 0x5d // Transient storage store (EIP-1153)
	MCOPY    OpCode = 0x5e // Memory copy (EIP-5656)
)

// === Push Operations (0x5f - 0x7f) ===
const (
	PUSH0  OpCode = 0x5f // Push 0 onto stack (EIP-3855)
	PUSH1  OpCode = 0x60 // Push 1 byte onto stack
	PUSH2  OpCode = 0x61 // Push 2 bytes onto stack
	PUSH3  OpCode = 0x62 // Push 3 bytes onto stack
	PUSH4  OpCode = 0x63 // Push 4 bytes onto stack
	PUSH5  OpCode = 0x64 // Push 5 bytes onto stack
	PUSH6  OpCode = 0x65 // Push 6 bytes onto stack
	PUSH7  OpCode = 0x66 // Push 7 bytes onto stack
	PUSH8  OpCode = 0x67 // Push 8 bytes onto stack
	PUSH9  OpCode = 0x68 // Push 9 bytes onto stack
	PUSH10 OpCode = 0x69 // Push 10 bytes onto stack
	PUSH11 OpCode = 0x6a // Push 11 bytes onto stack
	PUSH12 OpCode = 0x6b // Push 12 bytes onto stack
	PUSH13 OpCode = 0x6c // Push 13 bytes onto stack
	PUSH14 OpCode = 0x6d // Push 14 bytes onto stack
	PUSH15 OpCode = 0x6e // Push 15 bytes onto stack
	PUSH16 OpCode = 0x6f // Push 16 bytes onto stack
	PUSH17 OpCode = 0x70 // Push 17 bytes onto stack
	PUSH18 OpCode = 0x71 // Push 18 bytes onto stack
	PUSH19 OpCode = 0x72 // Push 19 bytes onto stack
	PUSH20 OpCode = 0x73 // Push 20 bytes onto stack
	PUSH21 OpCode = 0x74 // Push 21 bytes onto stack
	PUSH22 OpCode = 0x75 // Push 22 bytes onto stack
	PUSH23 OpCode = 0x76 // Push 23 bytes onto stack
	PUSH24 OpCode = 0x77 // Push 24 bytes onto stack
	PUSH25 OpCode = 0x78 // Push 25 bytes onto stack
	PUSH26 OpCode = 0x79 // Push 26 bytes onto stack
	PUSH27 OpCode = 0x7a // Push 27 bytes onto stack
	PUSH28 OpCode = 0x7b // Push 28 bytes onto stack
	PUSH29 OpCode = 0x7c // Push 29 bytes onto stack
	PUSH30 OpCode = 0x7d // Push 30 bytes onto stack
	PUSH31 OpCode = 0x7e // Push 31 bytes onto stack
	PUSH32 OpCode = 0x7f // Push 32 bytes onto stack
)

// === Duplicate Operations (0x80 - 0x8f) ===
const (
	DUP1  OpCode = 0x80 // Duplicate 1st stack item
	DUP2  OpCode = 0x81 // Duplicate 2nd stack item
	DUP3  OpCode = 0x82 // Duplicate 3rd stack item
	DUP4  OpCode = 0x83 // Duplicate 4th stack item
	DUP5  OpCode = 0x84 // Duplicate 5th stack item
	DUP6  OpCode = 0x85 // Duplicate 6th stack item
	DUP7  OpCode = 0x86 // Duplicate 7th stack item
	DUP8  OpCode = 0x87 // Duplicate 8th stack item
	DUP9  OpCode = 0x88 // Duplicate 9th stack item
	DUP10 OpCode = 0x89 // Duplicate 10th stack item
	DUP11 OpCode = 0x8a // Duplicate 11th stack item
	DUP12 OpCode = 0x8b // Duplicate 12th stack item
	DUP13 OpCode = 0x8c // Duplicate 13th stack item
	DUP14 OpCode = 0x8d // Duplicate 14th stack item
	DUP15 OpCode = 0x8e // Duplicate 15th stack item
	DUP16 OpCode = 0x8f // Duplicate 16th stack item
)

// === Swap Operations (0x90 - 0x9f) ===
const (
	SWAP1  OpCode = 0x90 // Exchange 1st and 2nd stack items
	SWAP2  OpCode = 0x91 // Exchange 1st and 3rd stack items
	SWAP3  OpCode = 0x92 // Exchange 1st and 4th stack items
	SWAP4  OpCode = 0x93 // Exchange 1st and 5th stack items
	SWAP5  OpCode = 0x94 // Exchange 1st and 6th stack items
	SWAP6  OpCode = 0x95 // Exchange 1st and 7th stack items
	SWAP7  OpCode = 0x96 // Exchange 1st and 8th stack items
	SWAP8  OpCode = 0x97 // Exchange 1st and 9th stack items
	SWAP9  OpCode = 0x98 // Exchange 1st and 10th stack items
	SWAP10 OpCode = 0x99 // Exchange 1st and 11th stack items
	SWAP11 OpCode = 0x9a // Exchange 1st and 12th stack items
	SWAP12 OpCode = 0x9b // Exchange 1st and 13th stack items
	SWAP13 OpCode = 0x9c // Exchange 1st and 14th stack items
	SWAP14 OpCode = 0x9d // Exchange 1st and 15th stack items
	SWAP15 OpCode = 0x9e // Exchange 1st and 16th stack items
	SWAP16 OpCode = 0x9f // Exchange 1st and 17th stack items
)

// === Log Operations (0xa0 - 0xa4) ===
const (
	LOG0 OpCode = 0xa0 // Append log record with 0 topics
	LOG1 OpCode = 0xa1 // Append log record with 1 topic
	LOG2 OpCode = 0xa2 // Append log record with 2 topics
	LOG3 OpCode = 0xa3 // Append log record with 3 topics
	LOG4 OpCode = 0xa4 // Append log record with 4 topics
)

// === System Operations (0xf0 - 0xff) ===
const (
	CREATE       OpCode = 0xf0 // Create new contract
	CALL         OpCode = 0xf1 // Message call
	CALLCODE     OpCode = 0xf2 // Message call with own storage (deprecated)
	RETURN       OpCode = 0xf3 // Halt and return data
	DELEGATECALL OpCode = 0xf4 // Message call with caller's context
	CREATE2      OpCode = 0xf5 // Create with salt (EIP-1014)
	STATICCALL   OpCode = 0xfa // Static message call (EIP-214)
	REVERT       OpCode = 0xfd // Halt and revert state changes
	INVALID      OpCode = 0xfe // Invalid instruction
	SELFDESTRUCT OpCode = 0xff // Destroy contract (deprecated in EIP-6780)
)

// String returns the opcode name.
func (op OpCode) String() string {
	if name, ok := opCodeNames[op]; ok {
		return name
	}
	return "UNKNOWN"
}

// IsPush returns true if the opcode is a PUSH instruction.
func (op OpCode) IsPush() bool {
	return op >= PUSH1 && op <= PUSH32
}

// PushBytes returns the number of bytes pushed for PUSH opcodes.
func (op OpCode) PushBytes() int {
	if op == PUSH0 {
		return 0
	}
	if op >= PUSH1 && op <= PUSH32 {
		return int(op - PUSH1 + 1)
	}
	return 0
}

// IsDup returns true if the opcode is a DUP instruction.
func (op OpCode) IsDup() bool {
	return op >= DUP1 && op <= DUP16
}

// DupPosition returns the stack position for DUP opcodes (1-16).
func (op OpCode) DupPosition() int {
	if op >= DUP1 && op <= DUP16 {
		return int(op - DUP1 + 1)
	}
	return 0
}

// IsSwap returns true if the opcode is a SWAP instruction.
func (op OpCode) IsSwap() bool {
	return op >= SWAP1 && op <= SWAP16
}

// SwapPosition returns the stack position for SWAP opcodes (1-16).
func (op OpCode) SwapPosition() int {
	if op >= SWAP1 && op <= SWAP16 {
		return int(op - SWAP1 + 1)
	}
	return 0
}

// IsLog returns true if the opcode is a LOG instruction.
func (op OpCode) IsLog() bool {
	return op >= LOG0 && op <= LOG4
}

// LogTopics returns the number of topics for LOG opcodes.
func (op OpCode) LogTopics() int {
	if op >= LOG0 && op <= LOG4 {
		return int(op - LOG0)
	}
	return 0
}

// IsCall returns true if the opcode makes external calls.
func (op OpCode) IsCall() bool {
	return op == CALL || op == CALLCODE || op == DELEGATECALL || op == STATICCALL
}

// IsCreate returns true if the opcode creates contracts.
func (op OpCode) IsCreate() bool {
	return op == CREATE || op == CREATE2
}

// IsStaticViolation returns true if the opcode modifies state (invalid in STATICCALL).
func (op OpCode) IsStaticViolation() bool {
	switch op {
	case SSTORE, CREATE, CREATE2, SELFDESTRUCT, LOG0, LOG1, LOG2, LOG3, LOG4, CALL:
		return true
	}
	return false
}

// opCodeNames maps opcodes to their names.
var opCodeNames = map[OpCode]string{
	STOP:           "STOP",
	ADD:            "ADD",
	MUL:            "MUL",
	SUB:            "SUB",
	DIV:            "DIV",
	SDIV:           "SDIV",
	MOD:            "MOD",
	SMOD:           "SMOD",
	ADDMOD:         "ADDMOD",
	MULMOD:         "MULMOD",
	EXP:            "EXP",
	SIGNEXTEND:     "SIGNEXTEND",
	LT:             "LT",
	GT:             "GT",
	SLT:            "SLT",
	SGT:            "SGT",
	EQ:             "EQ",
	ISZERO:         "ISZERO",
	AND:            "AND",
	OR:             "OR",
	XOR:            "XOR",
	NOT:            "NOT",
	BYTE:           "BYTE",
	SHL:            "SHL",
	SHR:            "SHR",
	SAR:            "SAR",
	KECCAK256:      "KECCAK256",
	ADDRESS:        "ADDRESS",
	BALANCE:        "BALANCE",
	ORIGIN:         "ORIGIN",
	CALLER:         "CALLER",
	CALLVALUE:      "CALLVALUE",
	CALLDATALOAD:   "CALLDATALOAD",
	CALLDATASIZE:   "CALLDATASIZE",
	CALLDATACOPY:   "CALLDATACOPY",
	CODESIZE:       "CODESIZE",
	CODECOPY:       "CODECOPY",
	GASPRICE:       "GASPRICE",
	EXTCODESIZE:    "EXTCODESIZE",
	EXTCODECOPY:    "EXTCODECOPY",
	RETURNDATASIZE: "RETURNDATASIZE",
	RETURNDATACOPY: "RETURNDATACOPY",
	EXTCODEHASH:    "EXTCODEHASH",
	BLOCKHASH:      "BLOCKHASH",
	COINBASE:       "COINBASE",
	TIMESTAMP:      "TIMESTAMP",
	NUMBER:         "NUMBER",
	PREVRANDAO:     "PREVRANDAO",
	GASLIMIT:       "GASLIMIT",
	CHAINID:        "CHAINID",
	SELFBALANCE:    "SELFBALANCE",
	BASEFEE:        "BASEFEE",
	BLOBHASH:       "BLOBHASH",
	BLOBBASEFEE:    "BLOBBASEFEE",
	POP:            "POP",
	MLOAD:          "MLOAD",
	MSTORE:         "MSTORE",
	MSTORE8:        "MSTORE8",
	SLOAD:          "SLOAD",
	SSTORE:         "SSTORE",
	JUMP:           "JUMP",
	JUMPI:          "JUMPI",
	PC:             "PC",
	MSIZE:          "MSIZE",
	GAS:            "GAS",
	JUMPDEST:       "JUMPDEST",
	TLOAD:          "TLOAD",
	TSTORE:         "TSTORE",
	MCOPY:          "MCOPY",
	PUSH0:          "PUSH0",
	PUSH1:          "PUSH1",
	PUSH2:          "PUSH2",
	PUSH3:          "PUSH3",
	PUSH4:          "PUSH4",
	PUSH5:          "PUSH5",
	PUSH6:          "PUSH6",
	PUSH7:          "PUSH7",
	PUSH8:          "PUSH8",
	PUSH9:          "PUSH9",
	PUSH10:         "PUSH10",
	PUSH11:         "PUSH11",
	PUSH12:         "PUSH12",
	PUSH13:         "PUSH13",
	PUSH14:         "PUSH14",
	PUSH15:         "PUSH15",
	PUSH16:         "PUSH16",
	PUSH17:         "PUSH17",
	PUSH18:         "PUSH18",
	PUSH19:         "PUSH19",
	PUSH20:         "PUSH20",
	PUSH21:         "PUSH21",
	PUSH22:         "PUSH22",
	PUSH23:         "PUSH23",
	PUSH24:         "PUSH24",
	PUSH25:         "PUSH25",
	PUSH26:         "PUSH26",
	PUSH27:         "PUSH27",
	PUSH28:         "PUSH28",
	PUSH29:         "PUSH29",
	PUSH30:         "PUSH30",
	PUSH31:         "PUSH31",
	PUSH32:         "PUSH32",
	DUP1:           "DUP1",
	DUP2:           "DUP2",
	DUP3:           "DUP3",
	DUP4:           "DUP4",
	DUP5:           "DUP5",
	DUP6:           "DUP6",
	DUP7:           "DUP7",
	DUP8:           "DUP8",
	DUP9:           "DUP9",
	DUP10:          "DUP10",
	DUP11:          "DUP11",
	DUP12:          "DUP12",
	DUP13:          "DUP13",
	DUP14:          "DUP14",
	DUP15:          "DUP15",
	DUP16:          "DUP16",
	SWAP1:          "SWAP1",
	SWAP2:          "SWAP2",
	SWAP3:          "SWAP3",
	SWAP4:          "SWAP4",
	SWAP5:          "SWAP5",
	SWAP6:          "SWAP6",
	SWAP7:          "SWAP7",
	SWAP8:          "SWAP8",
	SWAP9:          "SWAP9",
	SWAP10:         "SWAP10",
	SWAP11:         "SWAP11",
	SWAP12:         "SWAP12",
	SWAP13:         "SWAP13",
	SWAP14:         "SWAP14",
	SWAP15:         "SWAP15",
	SWAP16:         "SWAP16",
	LOG0:           "LOG0",
	LOG1:           "LOG1",
	LOG2:           "LOG2",
	LOG3:           "LOG3",
	LOG4:           "LOG4",
	CREATE:         "CREATE",
	CALL:           "CALL",
	CALLCODE:       "CALLCODE",
	RETURN:         "RETURN",
	DELEGATECALL:   "DELEGATECALL",
	CREATE2:        "CREATE2",
	STATICCALL:     "STATICCALL",
	REVERT:         "REVERT",
	INVALID:        "INVALID",
	SELFDESTRUCT:   "SELFDESTRUCT",
}

// === Gas Costs ===

// GasCosts defines the gas cost for each opcode.
type GasCosts struct {
	// Base costs
	Zero           uint64 // STOP, RETURN, REVERT
	Base           uint64 // ADD, SUB, LT, GT, etc.
	VeryLow        uint64 // PUSH, DUP, SWAP
	Low            uint64 // MUL, DIV, etc.
	Mid            uint64 // ADDMOD, MULMOD
	High           uint64 // JUMP
	Ext            uint64 // EXTCODE*, BALANCE
	ExtCodeHash    uint64 // EXTCODEHASH (EIP-1884)
	Balance        uint64 // BALANCE (EIP-1884)
	SLoad          uint64 // SLOAD (EIP-2200)
	JumpDest       uint64 // JUMPDEST
	SSet           uint64 // SSTORE set (EIP-2200)
	SReset         uint64 // SSTORE reset (EIP-2200)
	SClear         uint64 // SSTORE clear refund
	SelfDestruct   uint64 // SELFDESTRUCT
	Create         uint64 // CREATE
	CodeDeposit    uint64 // Per byte for CREATE
	Call           uint64 // CALL
	CallValue      uint64 // CALL with value transfer
	CallStipend    uint64 // Stipend for CALL with value
	NewAccount     uint64 // Creating new account
	Exp            uint64 // EXP base
	ExpByte        uint64 // Per byte of exponent
	Memory         uint64 // Per word for memory expansion
	TxCreate       uint64 // CREATE transaction
	TxDataZero     uint64 // Per zero byte of tx data
	TxDataNonZero  uint64 // Per non-zero byte of tx data
	Transaction    uint64 // Transaction base cost
	Log            uint64 // LOG base
	LogTopic       uint64 // Per LOG topic
	LogData        uint64 // Per byte of LOG data
	Copy           uint64 // Per word for COPY operations
	Keccak256      uint64 // KECCAK256 base
	Keccak256Word  uint64 // Per word for KECCAK256
	SelfDestRefund uint64 // SELFDESTRUCT refund
	ColdSLoad      uint64 // Cold SLOAD (EIP-2929)
	ColdAccount    uint64 // Cold account access (EIP-2929)
	WarmAccess     uint64 // Warm access (EIP-2929)
}

// DefaultGasCosts returns gas costs for the latest EVM version.
func DefaultGasCosts() *GasCosts {
	return &GasCosts{
		Zero:           0,
		Base:           2,
		VeryLow:        3,
		Low:            5,
		Mid:            8,
		High:           10,
		Ext:            0, // Handled by EIP-2929
		ExtCodeHash:    0, // Handled by EIP-2929
		Balance:        0, // Handled by EIP-2929
		SLoad:          0, // Handled by EIP-2929
		JumpDest:       1,
		SSet:           20000,
		SReset:         2900,
		SClear:         4800,
		SelfDestruct:   5000,
		Create:         32000,
		CodeDeposit:    200,
		Call:           0, // Handled by EIP-2929
		CallValue:      9000,
		CallStipend:    2300,
		NewAccount:     25000,
		Exp:            10,
		ExpByte:        50,
		Memory:         3,
		TxCreate:       32000,
		TxDataZero:     4,
		TxDataNonZero:  16,
		Transaction:    21000,
		Log:            375,
		LogTopic:       375,
		LogData:        8,
		Copy:           3,
		Keccak256:      30,
		Keccak256Word:  6,
		SelfDestRefund: 24000,
		ColdSLoad:      2100,
		ColdAccount:    2600,
		WarmAccess:     100,
	}
}

// === Opcode Metadata ===

// OpInfo contains metadata about an opcode.
type OpInfo struct {
	Name       string // Opcode name
	StackPop   int    // Number of items popped from stack
	StackPush  int    // Number of items pushed to stack
	Gas        uint64 // Base gas cost (static component)
	MemorySize int    // Memory size function (0 = none, 1 = simple, 2 = complex)
	Valid      bool   // Whether the opcode is valid
	Halts      bool   // Whether execution halts
	Jumps      bool   // Whether PC changes non-sequentially
	Writes     bool   // Whether state is modified
	Returns    bool   // Whether there is return data
}

// OpTable maps opcodes to their metadata.
var OpTable = [256]OpInfo{
	STOP:       {"STOP", 0, 0, 0, 0, true, true, false, false, false},
	ADD:        {"ADD", 2, 1, 3, 0, true, false, false, false, false},
	MUL:        {"MUL", 2, 1, 5, 0, true, false, false, false, false},
	SUB:        {"SUB", 2, 1, 3, 0, true, false, false, false, false},
	DIV:        {"DIV", 2, 1, 5, 0, true, false, false, false, false},
	SDIV:       {"SDIV", 2, 1, 5, 0, true, false, false, false, false},
	MOD:        {"MOD", 2, 1, 5, 0, true, false, false, false, false},
	SMOD:       {"SMOD", 2, 1, 5, 0, true, false, false, false, false},
	ADDMOD:     {"ADDMOD", 3, 1, 8, 0, true, false, false, false, false},
	MULMOD:     {"MULMOD", 3, 1, 8, 0, true, false, false, false, false},
	EXP:        {"EXP", 2, 1, 10, 0, true, false, false, false, false},
	SIGNEXTEND: {"SIGNEXTEND", 2, 1, 5, 0, true, false, false, false, false},

	LT:     {"LT", 2, 1, 3, 0, true, false, false, false, false},
	GT:     {"GT", 2, 1, 3, 0, true, false, false, false, false},
	SLT:    {"SLT", 2, 1, 3, 0, true, false, false, false, false},
	SGT:    {"SGT", 2, 1, 3, 0, true, false, false, false, false},
	EQ:     {"EQ", 2, 1, 3, 0, true, false, false, false, false},
	ISZERO: {"ISZERO", 1, 1, 3, 0, true, false, false, false, false},
	AND:    {"AND", 2, 1, 3, 0, true, false, false, false, false},
	OR:     {"OR", 2, 1, 3, 0, true, false, false, false, false},
	XOR:    {"XOR", 2, 1, 3, 0, true, false, false, false, false},
	NOT:    {"NOT", 1, 1, 3, 0, true, false, false, false, false},
	BYTE:   {"BYTE", 2, 1, 3, 0, true, false, false, false, false},
	SHL:    {"SHL", 2, 1, 3, 0, true, false, false, false, false},
	SHR:    {"SHR", 2, 1, 3, 0, true, false, false, false, false},
	SAR:    {"SAR", 2, 1, 3, 0, true, false, false, false, false},

	KECCAK256: {"KECCAK256", 2, 1, 30, 1, true, false, false, false, false},

	ADDRESS:        {"ADDRESS", 0, 1, 2, 0, true, false, false, false, false},
	BALANCE:        {"BALANCE", 1, 1, 0, 0, true, false, false, false, false},
	ORIGIN:         {"ORIGIN", 0, 1, 2, 0, true, false, false, false, false},
	CALLER:         {"CALLER", 0, 1, 2, 0, true, false, false, false, false},
	CALLVALUE:      {"CALLVALUE", 0, 1, 2, 0, true, false, false, false, false},
	CALLDATALOAD:   {"CALLDATALOAD", 1, 1, 3, 0, true, false, false, false, false},
	CALLDATASIZE:   {"CALLDATASIZE", 0, 1, 2, 0, true, false, false, false, false},
	CALLDATACOPY:   {"CALLDATACOPY", 3, 0, 3, 1, true, false, false, false, false},
	CODESIZE:       {"CODESIZE", 0, 1, 2, 0, true, false, false, false, false},
	CODECOPY:       {"CODECOPY", 3, 0, 3, 1, true, false, false, false, false},
	GASPRICE:       {"GASPRICE", 0, 1, 2, 0, true, false, false, false, false},
	EXTCODESIZE:    {"EXTCODESIZE", 1, 1, 0, 0, true, false, false, false, false},
	EXTCODECOPY:    {"EXTCODECOPY", 4, 0, 0, 1, true, false, false, false, false},
	RETURNDATASIZE: {"RETURNDATASIZE", 0, 1, 2, 0, true, false, false, false, false},
	RETURNDATACOPY: {"RETURNDATACOPY", 3, 0, 3, 1, true, false, false, false, false},
	EXTCODEHASH:    {"EXTCODEHASH", 1, 1, 0, 0, true, false, false, false, false},

	BLOCKHASH:   {"BLOCKHASH", 1, 1, 20, 0, true, false, false, false, false},
	COINBASE:    {"COINBASE", 0, 1, 2, 0, true, false, false, false, false},
	TIMESTAMP:   {"TIMESTAMP", 0, 1, 2, 0, true, false, false, false, false},
	NUMBER:      {"NUMBER", 0, 1, 2, 0, true, false, false, false, false},
	PREVRANDAO:  {"PREVRANDAO", 0, 1, 2, 0, true, false, false, false, false},
	GASLIMIT:    {"GASLIMIT", 0, 1, 2, 0, true, false, false, false, false},
	CHAINID:     {"CHAINID", 0, 1, 2, 0, true, false, false, false, false},
	SELFBALANCE: {"SELFBALANCE", 0, 1, 5, 0, true, false, false, false, false},
	BASEFEE:     {"BASEFEE", 0, 1, 2, 0, true, false, false, false, false},
	BLOBHASH:    {"BLOBHASH", 1, 1, 3, 0, true, false, false, false, false},
	BLOBBASEFEE: {"BLOBBASEFEE", 0, 1, 2, 0, true, false, false, false, false},

	POP:      {"POP", 1, 0, 2, 0, true, false, false, false, false},
	MLOAD:    {"MLOAD", 1, 1, 3, 1, true, false, false, false, false},
	MSTORE:   {"MSTORE", 2, 0, 3, 1, true, false, false, false, false},
	MSTORE8:  {"MSTORE8", 2, 0, 3, 1, true, false, false, false, false},
	SLOAD:    {"SLOAD", 1, 1, 0, 0, true, false, false, false, false},
	SSTORE:   {"SSTORE", 2, 0, 0, 0, true, false, false, true, false},
	JUMP:     {"JUMP", 1, 0, 8, 0, true, false, true, false, false},
	JUMPI:    {"JUMPI", 2, 0, 10, 0, true, false, true, false, false},
	PC:       {"PC", 0, 1, 2, 0, true, false, false, false, false},
	MSIZE:    {"MSIZE", 0, 1, 2, 0, true, false, false, false, false},
	GAS:      {"GAS", 0, 1, 2, 0, true, false, false, false, false},
	JUMPDEST: {"JUMPDEST", 0, 0, 1, 0, true, false, false, false, false},
	TLOAD:    {"TLOAD", 1, 1, 100, 0, true, false, false, false, false},
	TSTORE:   {"TSTORE", 2, 0, 100, 0, true, false, false, true, false},
	MCOPY:    {"MCOPY", 3, 0, 3, 1, true, false, false, false, false},

	PUSH0:  {"PUSH0", 0, 1, 2, 0, true, false, false, false, false},
	PUSH1:  {"PUSH1", 0, 1, 3, 0, true, false, false, false, false},
	PUSH2:  {"PUSH2", 0, 1, 3, 0, true, false, false, false, false},
	PUSH3:  {"PUSH3", 0, 1, 3, 0, true, false, false, false, false},
	PUSH4:  {"PUSH4", 0, 1, 3, 0, true, false, false, false, false},
	PUSH5:  {"PUSH5", 0, 1, 3, 0, true, false, false, false, false},
	PUSH6:  {"PUSH6", 0, 1, 3, 0, true, false, false, false, false},
	PUSH7:  {"PUSH7", 0, 1, 3, 0, true, false, false, false, false},
	PUSH8:  {"PUSH8", 0, 1, 3, 0, true, false, false, false, false},
	PUSH9:  {"PUSH9", 0, 1, 3, 0, true, false, false, false, false},
	PUSH10: {"PUSH10", 0, 1, 3, 0, true, false, false, false, false},
	PUSH11: {"PUSH11", 0, 1, 3, 0, true, false, false, false, false},
	PUSH12: {"PUSH12", 0, 1, 3, 0, true, false, false, false, false},
	PUSH13: {"PUSH13", 0, 1, 3, 0, true, false, false, false, false},
	PUSH14: {"PUSH14", 0, 1, 3, 0, true, false, false, false, false},
	PUSH15: {"PUSH15", 0, 1, 3, 0, true, false, false, false, false},
	PUSH16: {"PUSH16", 0, 1, 3, 0, true, false, false, false, false},
	PUSH17: {"PUSH17", 0, 1, 3, 0, true, false, false, false, false},
	PUSH18: {"PUSH18", 0, 1, 3, 0, true, false, false, false, false},
	PUSH19: {"PUSH19", 0, 1, 3, 0, true, false, false, false, false},
	PUSH20: {"PUSH20", 0, 1, 3, 0, true, false, false, false, false},
	PUSH21: {"PUSH21", 0, 1, 3, 0, true, false, false, false, false},
	PUSH22: {"PUSH22", 0, 1, 3, 0, true, false, false, false, false},
	PUSH23: {"PUSH23", 0, 1, 3, 0, true, false, false, false, false},
	PUSH24: {"PUSH24", 0, 1, 3, 0, true, false, false, false, false},
	PUSH25: {"PUSH25", 0, 1, 3, 0, true, false, false, false, false},
	PUSH26: {"PUSH26", 0, 1, 3, 0, true, false, false, false, false},
	PUSH27: {"PUSH27", 0, 1, 3, 0, true, false, false, false, false},
	PUSH28: {"PUSH28", 0, 1, 3, 0, true, false, false, false, false},
	PUSH29: {"PUSH29", 0, 1, 3, 0, true, false, false, false, false},
	PUSH30: {"PUSH30", 0, 1, 3, 0, true, false, false, false, false},
	PUSH31: {"PUSH31", 0, 1, 3, 0, true, false, false, false, false},
	PUSH32: {"PUSH32", 0, 1, 3, 0, true, false, false, false, false},

	DUP1:  {"DUP1", 1, 2, 3, 0, true, false, false, false, false},
	DUP2:  {"DUP2", 2, 3, 3, 0, true, false, false, false, false},
	DUP3:  {"DUP3", 3, 4, 3, 0, true, false, false, false, false},
	DUP4:  {"DUP4", 4, 5, 3, 0, true, false, false, false, false},
	DUP5:  {"DUP5", 5, 6, 3, 0, true, false, false, false, false},
	DUP6:  {"DUP6", 6, 7, 3, 0, true, false, false, false, false},
	DUP7:  {"DUP7", 7, 8, 3, 0, true, false, false, false, false},
	DUP8:  {"DUP8", 8, 9, 3, 0, true, false, false, false, false},
	DUP9:  {"DUP9", 9, 10, 3, 0, true, false, false, false, false},
	DUP10: {"DUP10", 10, 11, 3, 0, true, false, false, false, false},
	DUP11: {"DUP11", 11, 12, 3, 0, true, false, false, false, false},
	DUP12: {"DUP12", 12, 13, 3, 0, true, false, false, false, false},
	DUP13: {"DUP13", 13, 14, 3, 0, true, false, false, false, false},
	DUP14: {"DUP14", 14, 15, 3, 0, true, false, false, false, false},
	DUP15: {"DUP15", 15, 16, 3, 0, true, false, false, false, false},
	DUP16: {"DUP16", 16, 17, 3, 0, true, false, false, false, false},

	SWAP1:  {"SWAP1", 2, 2, 3, 0, true, false, false, false, false},
	SWAP2:  {"SWAP2", 3, 3, 3, 0, true, false, false, false, false},
	SWAP3:  {"SWAP3", 4, 4, 3, 0, true, false, false, false, false},
	SWAP4:  {"SWAP4", 5, 5, 3, 0, true, false, false, false, false},
	SWAP5:  {"SWAP5", 6, 6, 3, 0, true, false, false, false, false},
	SWAP6:  {"SWAP6", 7, 7, 3, 0, true, false, false, false, false},
	SWAP7:  {"SWAP7", 8, 8, 3, 0, true, false, false, false, false},
	SWAP8:  {"SWAP8", 9, 9, 3, 0, true, false, false, false, false},
	SWAP9:  {"SWAP9", 10, 10, 3, 0, true, false, false, false, false},
	SWAP10: {"SWAP10", 11, 11, 3, 0, true, false, false, false, false},
	SWAP11: {"SWAP11", 12, 12, 3, 0, true, false, false, false, false},
	SWAP12: {"SWAP12", 13, 13, 3, 0, true, false, false, false, false},
	SWAP13: {"SWAP13", 14, 14, 3, 0, true, false, false, false, false},
	SWAP14: {"SWAP14", 15, 15, 3, 0, true, false, false, false, false},
	SWAP15: {"SWAP15", 16, 16, 3, 0, true, false, false, false, false},
	SWAP16: {"SWAP16", 17, 17, 3, 0, true, false, false, false, false},

	LOG0: {"LOG0", 2, 0, 375, 1, true, false, false, true, false},
	LOG1: {"LOG1", 3, 0, 750, 1, true, false, false, true, false},
	LOG2: {"LOG2", 4, 0, 1125, 1, true, false, false, true, false},
	LOG3: {"LOG3", 5, 0, 1500, 1, true, false, false, true, false},
	LOG4: {"LOG4", 6, 0, 1875, 1, true, false, false, true, false},

	CREATE:       {"CREATE", 3, 1, 32000, 1, true, false, false, true, false},
	CALL:         {"CALL", 7, 1, 0, 2, true, false, false, false, true},
	CALLCODE:     {"CALLCODE", 7, 1, 0, 2, true, false, false, false, true},
	RETURN:       {"RETURN", 2, 0, 0, 1, true, true, false, false, true},
	DELEGATECALL: {"DELEGATECALL", 6, 1, 0, 2, true, false, false, false, true},
	CREATE2:      {"CREATE2", 4, 1, 32000, 1, true, false, false, true, false},
	STATICCALL:   {"STATICCALL", 6, 1, 0, 2, true, false, false, false, true},
	REVERT:       {"REVERT", 2, 0, 0, 1, true, true, false, false, true},
	INVALID:      {"INVALID", 0, 0, 0, 0, false, true, false, false, false},
	SELFDESTRUCT: {"SELFDESTRUCT", 1, 0, 5000, 0, true, true, false, true, false},
}
