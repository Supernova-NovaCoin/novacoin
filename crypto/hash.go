// Package crypto provides cryptographic primitives for the NovaCoin blockchain.
package crypto

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// Hash computes the SHA3-256 hash of the input data.
func Hash(data []byte) [32]byte {
	return sha3.Sum256(data)
}

// Hash512 computes the SHA3-512 hash of the input data.
func Hash512(data []byte) [64]byte {
	return sha3.Sum512(data)
}

// HashMultiple computes the SHA3-256 hash of multiple byte slices concatenated.
func HashMultiple(data ...[]byte) [32]byte {
	h := sha3.New256()
	for _, d := range data {
		h.Write(d)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// HashUint64 hashes a uint64 value.
func HashUint64(v uint64) [32]byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)
	return Hash(buf)
}

// CombineHashes combines multiple hashes into one.
func CombineHashes(hashes ...[32]byte) [32]byte {
	h := sha3.New256()
	for _, hash := range hashes {
		h.Write(hash[:])
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// MerkleRoot computes the Merkle root of a list of hashes.
func MerkleRoot(hashes [][32]byte) [32]byte {
	if len(hashes) == 0 {
		return [32]byte{}
	}
	if len(hashes) == 1 {
		return hashes[0]
	}

	// Pad to even number if necessary
	if len(hashes)%2 == 1 {
		hashes = append(hashes, hashes[len(hashes)-1])
	}

	// Build tree bottom-up
	for len(hashes) > 1 {
		var newLevel [][32]byte
		for i := 0; i < len(hashes); i += 2 {
			combined := CombineHashes(hashes[i], hashes[i+1])
			newLevel = append(newLevel, combined)
		}
		hashes = newLevel

		// Pad again if odd
		if len(hashes) > 1 && len(hashes)%2 == 1 {
			hashes = append(hashes, hashes[len(hashes)-1])
		}
	}

	return hashes[0]
}

// Keccak256 computes the Keccak-256 hash (Ethereum compatible).
func Keccak256(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// Keccak256Multiple computes Keccak-256 of multiple byte slices.
func Keccak256Multiple(data ...[]byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	for _, d := range data {
		h.Write(d)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// HashToAddress converts a hash to an address (last 20 bytes).
func HashToAddress(hash [32]byte) [20]byte {
	var addr [20]byte
	copy(addr[:], hash[12:])
	return addr
}

// PubKeyToAddress derives an address from a public key.
func PubKeyToAddress(pubKey []byte) [20]byte {
	hash := Keccak256(pubKey)
	return HashToAddress(hash)
}
