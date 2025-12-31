// Package p2p implements the peer-to-peer networking layer for NovaCoin.
package p2p

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/novacoin/novacoin/core/dag"
	"github.com/novacoin/novacoin/crypto"
)

// MessageType represents the type of P2P message.
type MessageType uint16

const (
	// Control messages (1-99)
	MsgPing         MessageType = 1
	MsgPong         MessageType = 2
	MsgHandshake    MessageType = 3
	MsgHandshakeAck MessageType = 4
	MsgDisconnect   MessageType = 5
	MsgGetPeers     MessageType = 6
	MsgPeers        MessageType = 7

	// Block messages (100-199)
	MsgNewBlock        MessageType = 100
	MsgGetBlocks       MessageType = 101
	MsgBlocks          MessageType = 102
	MsgGetBlockHeaders MessageType = 103
	MsgBlockHeaders    MessageType = 104
	MsgBlockRequest    MessageType = 105
	MsgBlockResponse   MessageType = 106

	// Transaction messages (200-299)
	MsgNewTransaction  MessageType = 200
	MsgGetTransactions MessageType = 201
	MsgTransactions    MessageType = 202
	MsgEncryptedTx     MessageType = 203

	// Consensus messages (300-399)
	MsgVote     MessageType = 300
	MsgProposal MessageType = 301
	MsgCommit   MessageType = 302
	MsgReveal   MessageType = 303
	MsgFinality MessageType = 304

	// Sync messages (400-499)
	MsgSyncRequest   MessageType = 400
	MsgSyncResponse  MessageType = 401
	MsgStateRequest  MessageType = 402
	MsgStateResponse MessageType = 403
)

func (t MessageType) String() string {
	switch t {
	case MsgPing:
		return "ping"
	case MsgPong:
		return "pong"
	case MsgHandshake:
		return "handshake"
	case MsgHandshakeAck:
		return "handshake_ack"
	case MsgDisconnect:
		return "disconnect"
	case MsgGetPeers:
		return "get_peers"
	case MsgPeers:
		return "peers"
	case MsgNewBlock:
		return "new_block"
	case MsgGetBlocks:
		return "get_blocks"
	case MsgBlocks:
		return "blocks"
	case MsgNewTransaction:
		return "new_tx"
	case MsgEncryptedTx:
		return "encrypted_tx"
	case MsgVote:
		return "vote"
	case MsgProposal:
		return "proposal"
	case MsgCommit:
		return "commit"
	case MsgReveal:
		return "reveal"
	case MsgFinality:
		return "finality"
	default:
		return "unknown"
	}
}

// Message represents a P2P message.
type Message struct {
	Type      MessageType
	ID        uint64    // Unique message ID for deduplication
	Timestamp time.Time
	Sender    PeerID
	Payload   []byte
	Signature []byte // Optional signature from sender
}

// MessageHeader is the fixed-size header for wire format.
type MessageHeader struct {
	Magic     uint32 // Network magic number
	Type      uint16
	ID        uint64
	Timestamp int64
	Length    uint32 // Payload length
	Checksum  [4]byte
}

const (
	// NetworkMagic identifies the network
	NetworkMagicMainnet = 0x4E4F5641 // "NOVA"
	NetworkMagicTestnet = 0x54455354 // "TEST"

	// MaxMessageSize is the maximum message size (10MB)
	MaxMessageSize = 10 * 1024 * 1024

	// HeaderSize is the size of MessageHeader
	HeaderSize = 4 + 2 + 8 + 8 + 4 + 4 // 30 bytes
)

// NewMessage creates a new message.
func NewMessage(msgType MessageType, payload []byte) *Message {
	return &Message{
		Type:      msgType,
		ID:        generateMessageID(),
		Timestamp: time.Now(),
		Payload:   payload,
	}
}

var messageIDCounter uint64

func generateMessageID() uint64 {
	messageIDCounter++
	return uint64(time.Now().UnixNano()) ^ messageIDCounter
}

// Hash returns the hash of the message.
func (m *Message) Hash() dag.Hash {
	data := make([]byte, 2+8+len(m.Payload))
	binary.BigEndian.PutUint16(data[0:2], uint16(m.Type))
	binary.BigEndian.PutUint64(data[2:10], m.ID)
	copy(data[10:], m.Payload)
	return crypto.Hash(data)
}

// Serialize serializes the message for wire transmission.
func (m *Message) Serialize(magic uint32) ([]byte, error) {
	if len(m.Payload) > MaxMessageSize {
		return nil, ErrMessageTooLarge
	}

	header := MessageHeader{
		Magic:     magic,
		Type:      uint16(m.Type),
		ID:        m.ID,
		Timestamp: m.Timestamp.UnixNano(),
		Length:    uint32(len(m.Payload)),
	}

	// Calculate checksum
	checksum := crypto.Hash(m.Payload)
	copy(header.Checksum[:], checksum[:4])

	// Serialize header
	data := make([]byte, HeaderSize+len(m.Payload))
	binary.BigEndian.PutUint32(data[0:4], header.Magic)
	binary.BigEndian.PutUint16(data[4:6], header.Type)
	binary.BigEndian.PutUint64(data[6:14], header.ID)
	binary.BigEndian.PutUint64(data[14:22], uint64(header.Timestamp))
	binary.BigEndian.PutUint32(data[22:26], header.Length)
	copy(data[26:30], header.Checksum[:])
	copy(data[30:], m.Payload)

	return data, nil
}

// DeserializeMessage deserializes a message from wire format.
func DeserializeMessage(data []byte, expectedMagic uint32) (*Message, error) {
	if len(data) < HeaderSize {
		return nil, ErrInvalidMessage
	}

	// Parse header
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != expectedMagic {
		return nil, ErrInvalidMagic
	}

	msgType := MessageType(binary.BigEndian.Uint16(data[4:6]))
	id := binary.BigEndian.Uint64(data[6:14])
	timestamp := int64(binary.BigEndian.Uint64(data[14:22]))
	length := binary.BigEndian.Uint32(data[22:26])
	var checksum [4]byte
	copy(checksum[:], data[26:30])

	if length > MaxMessageSize {
		return nil, ErrMessageTooLarge
	}

	if len(data) < HeaderSize+int(length) {
		return nil, ErrInvalidMessage
	}

	payload := data[30 : 30+length]

	// Verify checksum
	computed := crypto.Hash(payload)
	if checksum != [4]byte{computed[0], computed[1], computed[2], computed[3]} {
		return nil, ErrInvalidChecksum
	}

	return &Message{
		Type:      msgType,
		ID:        id,
		Timestamp: time.Unix(0, timestamp),
		Payload:   payload,
	}, nil
}

// === Specific Message Types ===

// HandshakeMessage is sent when connecting to a peer.
type HandshakeMessage struct {
	Version         uint32
	NetworkID       uint32
	NodeID          PeerID
	PublicKey       dag.PublicKey
	BestBlockHash   dag.Hash
	BestBlockHeight uint64
	Timestamp       int64
	UserAgent       string
	Capabilities    []string
	ValidatorIndex  *uint32 // nil if not a validator
}

// Serialize serializes the handshake message.
func (h *HandshakeMessage) Serialize() []byte {
	// Simple serialization: version(4) + networkID(4) + nodeID(32) + pubKey(48) +
	// bestHash(32) + bestHeight(8) + timestamp(8) + userAgentLen(2) + userAgent + caps
	size := 4 + 4 + 32 + 48 + 32 + 8 + 8 + 2 + len(h.UserAgent) + 4
	for _, cap := range h.Capabilities {
		size += 2 + len(cap)
	}
	if h.ValidatorIndex != nil {
		size += 5 // 1 byte flag + 4 bytes index
	} else {
		size += 1
	}

	data := make([]byte, size)
	offset := 0

	binary.BigEndian.PutUint32(data[offset:], h.Version)
	offset += 4
	binary.BigEndian.PutUint32(data[offset:], h.NetworkID)
	offset += 4
	copy(data[offset:], h.NodeID[:])
	offset += 32
	copy(data[offset:], h.PublicKey[:])
	offset += 48
	copy(data[offset:], h.BestBlockHash[:])
	offset += 32
	binary.BigEndian.PutUint64(data[offset:], h.BestBlockHeight)
	offset += 8
	binary.BigEndian.PutUint64(data[offset:], uint64(h.Timestamp))
	offset += 8
	binary.BigEndian.PutUint16(data[offset:], uint16(len(h.UserAgent)))
	offset += 2
	copy(data[offset:], h.UserAgent)
	offset += len(h.UserAgent)

	// Capabilities
	binary.BigEndian.PutUint32(data[offset:], uint32(len(h.Capabilities)))
	offset += 4
	for _, cap := range h.Capabilities {
		binary.BigEndian.PutUint16(data[offset:], uint16(len(cap)))
		offset += 2
		copy(data[offset:], cap)
		offset += len(cap)
	}

	// Validator index
	if h.ValidatorIndex != nil {
		data[offset] = 1
		offset++
		binary.BigEndian.PutUint32(data[offset:], *h.ValidatorIndex)
	} else {
		data[offset] = 0
	}

	return data
}

// PingMessage for latency measurement.
type PingMessage struct {
	Nonce     uint64
	Timestamp int64
}

// Serialize serializes the ping message.
func (p *PingMessage) Serialize() []byte {
	data := make([]byte, 16)
	binary.BigEndian.PutUint64(data[0:8], p.Nonce)
	binary.BigEndian.PutUint64(data[8:16], uint64(p.Timestamp))
	return data
}

// DeserializePing deserializes a ping message.
func DeserializePing(data []byte) (*PingMessage, error) {
	if len(data) < 16 {
		return nil, ErrInvalidMessage
	}
	return &PingMessage{
		Nonce:     binary.BigEndian.Uint64(data[0:8]),
		Timestamp: int64(binary.BigEndian.Uint64(data[8:16])),
	}, nil
}

// PongMessage is the response to ping.
type PongMessage struct {
	Nonce         uint64
	OrigTimestamp int64
	RecvTimestamp int64
}

// Serialize serializes the pong message.
func (p *PongMessage) Serialize() []byte {
	data := make([]byte, 24)
	binary.BigEndian.PutUint64(data[0:8], p.Nonce)
	binary.BigEndian.PutUint64(data[8:16], uint64(p.OrigTimestamp))
	binary.BigEndian.PutUint64(data[16:24], uint64(p.RecvTimestamp))
	return data
}

// DeserializePong deserializes a pong message.
func DeserializePong(data []byte) (*PongMessage, error) {
	if len(data) < 24 {
		return nil, ErrInvalidMessage
	}
	return &PongMessage{
		Nonce:         binary.BigEndian.Uint64(data[0:8]),
		OrigTimestamp: int64(binary.BigEndian.Uint64(data[8:16])),
		RecvTimestamp: int64(binary.BigEndian.Uint64(data[16:24])),
	}, nil
}

// BlockAnnouncement announces a new block.
type BlockAnnouncement struct {
	Hash      dag.Hash
	Height    uint64
	Timestamp int64
	Proposer  uint32
}

// Serialize serializes the block announcement.
func (b *BlockAnnouncement) Serialize() []byte {
	data := make([]byte, 52)
	copy(data[0:32], b.Hash[:])
	binary.BigEndian.PutUint64(data[32:40], b.Height)
	binary.BigEndian.PutUint64(data[40:48], uint64(b.Timestamp))
	binary.BigEndian.PutUint32(data[48:52], b.Proposer)
	return data
}

// DeserializeBlockAnnouncement deserializes a block announcement.
func DeserializeBlockAnnouncement(data []byte) (*BlockAnnouncement, error) {
	if len(data) < 52 {
		return nil, ErrInvalidMessage
	}
	var hash dag.Hash
	copy(hash[:], data[0:32])
	return &BlockAnnouncement{
		Hash:      hash,
		Height:    binary.BigEndian.Uint64(data[32:40]),
		Timestamp: int64(binary.BigEndian.Uint64(data[40:48])),
		Proposer:  binary.BigEndian.Uint32(data[48:52]),
	}, nil
}

// PeerInfo for peer discovery.
type PeerInfo struct {
	ID      PeerID
	Address string
	Score   int32
}

// Error types
var (
	ErrInvalidMessage  = errors.New("invalid message format")
	ErrInvalidMagic    = errors.New("invalid network magic")
	ErrInvalidChecksum = errors.New("invalid message checksum")
	ErrMessageTooLarge = errors.New("message too large")
)
