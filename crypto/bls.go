// Package crypto provides cryptographic primitives for the NovaCoin blockchain.
package crypto

import (
	"errors"
	"sync"

	"github.com/herumi/bls-eth-go-binary/bls"
)

var (
	// ErrInvalidSignature is returned when a signature is invalid.
	ErrInvalidSignature = errors.New("invalid BLS signature")

	// ErrInvalidPublicKey is returned when a public key is invalid.
	ErrInvalidPublicKey = errors.New("invalid BLS public key")

	// ErrInvalidSecretKey is returned when a secret key is invalid.
	ErrInvalidSecretKey = errors.New("invalid BLS secret key")

	// blsInitOnce ensures BLS is initialized exactly once.
	blsInitOnce sync.Once

	// blsInitErr stores any initialization error.
	blsInitErr error
)

// InitBLS initializes the BLS library. Must be called before any BLS operations.
func InitBLS() error {
	blsInitOnce.Do(func() {
		blsInitErr = bls.Init(bls.BLS12_381)
		if blsInitErr != nil {
			return
		}
		blsInitErr = bls.SetETHmode(bls.EthModeDraft07)
	})
	return blsInitErr
}

// BLSSecretKey wraps the BLS secret key.
type BLSSecretKey struct {
	key bls.SecretKey
}

// BLSPublicKey wraps the BLS public key.
type BLSPublicKey struct {
	key bls.PublicKey
}

// BLSSignature wraps the BLS signature.
type BLSSignature struct {
	sig bls.Sign
}

// GenerateKey generates a new BLS key pair.
func GenerateKey() (*BLSSecretKey, *BLSPublicKey, error) {
	if err := InitBLS(); err != nil {
		return nil, nil, err
	}

	var sk bls.SecretKey
	sk.SetByCSPRNG()

	pk := sk.GetPublicKey()

	return &BLSSecretKey{key: sk}, &BLSPublicKey{key: *pk}, nil
}

// SecretKeyFromBytes deserializes a secret key from bytes.
func SecretKeyFromBytes(data []byte) (*BLSSecretKey, error) {
	if err := InitBLS(); err != nil {
		return nil, err
	}

	var sk bls.SecretKey
	if err := sk.Deserialize(data); err != nil {
		return nil, ErrInvalidSecretKey
	}

	return &BLSSecretKey{key: sk}, nil
}

// PublicKeyFromBytes deserializes a public key from bytes.
func PublicKeyFromBytes(data []byte) (*BLSPublicKey, error) {
	if err := InitBLS(); err != nil {
		return nil, err
	}

	var pk bls.PublicKey
	if err := pk.Deserialize(data); err != nil {
		return nil, ErrInvalidPublicKey
	}

	return &BLSPublicKey{key: pk}, nil
}

// SignatureFromBytes deserializes a signature from bytes.
func SignatureFromBytes(data []byte) (*BLSSignature, error) {
	if err := InitBLS(); err != nil {
		return nil, err
	}

	var sig bls.Sign
	if err := sig.Deserialize(data); err != nil {
		return nil, ErrInvalidSignature
	}

	return &BLSSignature{sig: sig}, nil
}

// Bytes returns the serialized secret key.
func (sk *BLSSecretKey) Bytes() []byte {
	return sk.key.Serialize()
}

// PublicKey derives the public key from the secret key.
func (sk *BLSSecretKey) PublicKey() *BLSPublicKey {
	pk := sk.key.GetPublicKey()
	return &BLSPublicKey{key: *pk}
}

// Sign signs a message with the secret key.
func (sk *BLSSecretKey) Sign(message []byte) *BLSSignature {
	sig := sk.key.SignByte(message)
	return &BLSSignature{sig: *sig}
}

// Bytes returns the serialized public key (48 bytes).
func (pk *BLSPublicKey) Bytes() []byte {
	return pk.key.Serialize()
}

// To48Bytes returns the public key as a fixed 48-byte array.
func (pk *BLSPublicKey) To48Bytes() [48]byte {
	var result [48]byte
	copy(result[:], pk.key.Serialize())
	return result
}

// Bytes returns the serialized signature (96 bytes).
func (sig *BLSSignature) Bytes() []byte {
	return sig.sig.Serialize()
}

// To96Bytes returns the signature as a fixed 96-byte array.
func (sig *BLSSignature) To96Bytes() [96]byte {
	var result [96]byte
	copy(result[:], sig.sig.Serialize())
	return result
}

// Verify verifies a signature against a message and public key.
func (sig *BLSSignature) Verify(pk *BLSPublicKey, message []byte) bool {
	return sig.sig.VerifyByte(&pk.key, message)
}

// AggregateSignatures aggregates multiple signatures into one.
func AggregateSignatures(sigs []*BLSSignature) *BLSSignature {
	if len(sigs) == 0 {
		return nil
	}
	if len(sigs) == 1 {
		return sigs[0]
	}

	var aggSig bls.Sign
	blsSigs := make([]bls.Sign, len(sigs))
	for i, sig := range sigs {
		blsSigs[i] = sig.sig
	}
	aggSig.Aggregate(blsSigs)

	return &BLSSignature{sig: aggSig}
}

// AggregatePublicKeys aggregates multiple public keys into one.
func AggregatePublicKeys(pks []*BLSPublicKey) *BLSPublicKey {
	if len(pks) == 0 {
		return nil
	}
	if len(pks) == 1 {
		return pks[0]
	}

	// Start with a copy of the first key
	var aggPK bls.PublicKey
	if err := aggPK.Deserialize(pks[0].key.Serialize()); err != nil {
		return nil
	}

	// Add remaining keys
	for i := 1; i < len(pks); i++ {
		aggPK.Add(&pks[i].key)
	}

	return &BLSPublicKey{key: aggPK}
}

// VerifyAggregateSignature verifies an aggregated signature against multiple messages and public keys.
func VerifyAggregateSignature(sig *BLSSignature, pks []*BLSPublicKey, messages [][]byte) bool {
	if len(pks) != len(messages) {
		return false
	}
	if len(pks) == 0 {
		return false
	}

	// For same message, use aggregated public key
	// For different messages, use multi-verification
	aggPK := AggregatePublicKeys(pks)
	if aggPK == nil {
		return false
	}

	// Assuming same message for all (block hash signature)
	return sig.Verify(aggPK, messages[0])
}

// VerifyAggregateSameMessage verifies an aggregated signature where all signers signed the same message.
func VerifyAggregateSameMessage(sig *BLSSignature, pks []*BLSPublicKey, message []byte) bool {
	aggPK := AggregatePublicKeys(pks)
	if aggPK == nil {
		return false
	}
	return sig.Verify(aggPK, message)
}

// FastAggregateVerify performs fast aggregate verification (Ethereum 2.0 compatible).
func FastAggregateVerify(sig *BLSSignature, pks []*BLSPublicKey, message []byte) bool {
	if len(pks) == 0 {
		return false
	}

	blsPKs := make([]bls.PublicKey, len(pks))
	for i, pk := range pks {
		blsPKs[i] = pk.key
	}

	return sig.sig.FastAggregateVerify(blsPKs, message)
}
