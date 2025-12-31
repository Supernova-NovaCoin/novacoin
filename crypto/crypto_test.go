package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHash(t *testing.T) {
	data := []byte("hello world")
	hash := Hash(data)

	// Hash should be 32 bytes
	assert.Len(t, hash, 32)

	// Same input should produce same output
	hash2 := Hash(data)
	assert.Equal(t, hash, hash2)

	// Different input should produce different output
	hash3 := Hash([]byte("different"))
	assert.NotEqual(t, hash, hash3)
}

func TestHashMultiple(t *testing.T) {
	h1 := HashMultiple([]byte("a"), []byte("b"), []byte("c"))
	h2 := HashMultiple([]byte("abc"))

	// HashMultiple concatenates then hashes, so these should be equal
	assert.Equal(t, h1, h2)

	// Same inputs should produce same output
	h3 := HashMultiple([]byte("a"), []byte("b"), []byte("c"))
	assert.Equal(t, h1, h3)

	// Different order should produce different result
	h4 := HashMultiple([]byte("c"), []byte("b"), []byte("a"))
	assert.NotEqual(t, h1, h4)
}

func TestCombineHashes(t *testing.T) {
	h1 := Hash([]byte("first"))
	h2 := Hash([]byte("second"))

	combined := CombineHashes(h1, h2)
	assert.Len(t, combined, 32)

	// Order matters
	combined2 := CombineHashes(h2, h1)
	assert.NotEqual(t, combined, combined2)
}

func TestMerkleRoot(t *testing.T) {
	// Empty list
	root := MerkleRoot(nil)
	assert.Equal(t, [32]byte{}, root)

	// Single element
	h1 := Hash([]byte("one"))
	root = MerkleRoot([][32]byte{h1})
	assert.Equal(t, h1, root)

	// Two elements
	h2 := Hash([]byte("two"))
	root = MerkleRoot([][32]byte{h1, h2})
	assert.NotEqual(t, h1, root)
	assert.NotEqual(t, h2, root)

	// Three elements (odd number)
	h3 := Hash([]byte("three"))
	root = MerkleRoot([][32]byte{h1, h2, h3})
	assert.Len(t, root, 32)
}

func TestKeccak256(t *testing.T) {
	data := []byte("hello")
	hash := Keccak256(data)

	assert.Len(t, hash, 32)

	// Keccak256 is different from SHA3-256
	sha3Hash := Hash(data)
	assert.NotEqual(t, hash, sha3Hash)
}

func TestBLSKeyGeneration(t *testing.T) {
	sk, pk, err := GenerateKey()
	require.NoError(t, err)
	require.NotNil(t, sk)
	require.NotNil(t, pk)

	// Keys should be serializable
	skBytes := sk.Bytes()
	pkBytes := pk.Bytes()

	assert.True(t, len(skBytes) > 0)
	assert.Len(t, pkBytes, 48) // BLS public key is 48 bytes

	// Derive public key from secret key
	derivedPK := sk.PublicKey()
	assert.Equal(t, pk.Bytes(), derivedPK.Bytes())
}

func TestBLSSignAndVerify(t *testing.T) {
	sk, pk, err := GenerateKey()
	require.NoError(t, err)

	message := []byte("test message to sign")

	// Sign
	sig := sk.Sign(message)
	require.NotNil(t, sig)

	sigBytes := sig.Bytes()
	assert.Len(t, sigBytes, 96) // BLS signature is 96 bytes

	// Verify
	valid := sig.Verify(pk, message)
	assert.True(t, valid)

	// Wrong message should fail
	wrongMessage := []byte("wrong message")
	valid = sig.Verify(pk, wrongMessage)
	assert.False(t, valid)

	// Wrong key should fail
	_, wrongPK, _ := GenerateKey()
	valid = sig.Verify(wrongPK, message)
	assert.False(t, valid)
}

func TestBLSKeyDeserialization(t *testing.T) {
	sk, pk, err := GenerateKey()
	require.NoError(t, err)

	// Deserialize secret key
	sk2, err := SecretKeyFromBytes(sk.Bytes())
	require.NoError(t, err)
	assert.Equal(t, sk.Bytes(), sk2.Bytes())

	// Deserialize public key
	pk2, err := PublicKeyFromBytes(pk.Bytes())
	require.NoError(t, err)
	assert.Equal(t, pk.Bytes(), pk2.Bytes())

	// Verify signature with deserialized keys
	message := []byte("test")
	sig := sk.Sign(message)

	sig2, err := SignatureFromBytes(sig.Bytes())
	require.NoError(t, err)
	assert.True(t, sig2.Verify(pk2, message))
}

func TestBLSAggregation(t *testing.T) {
	message := []byte("common message")

	// Generate multiple key pairs
	var sks []*BLSSecretKey
	var pks []*BLSPublicKey
	var sigs []*BLSSignature

	for i := 0; i < 3; i++ {
		sk, pk, err := GenerateKey()
		require.NoError(t, err)
		sks = append(sks, sk)
		pks = append(pks, pk)
		sigs = append(sigs, sk.Sign(message))
	}

	// Aggregate signatures
	aggSig := AggregateSignatures(sigs)
	require.NotNil(t, aggSig)

	// Verify with aggregated public key
	aggPK := AggregatePublicKeys(pks)
	require.NotNil(t, aggPK)

	valid := aggSig.Verify(aggPK, message)
	assert.True(t, valid)
}

func TestBLSFastAggregateVerify(t *testing.T) {
	message := []byte("fast aggregate test")

	var pks []*BLSPublicKey
	var sigs []*BLSSignature

	for i := 0; i < 5; i++ {
		sk, pk, err := GenerateKey()
		require.NoError(t, err)
		pks = append(pks, pk)
		sigs = append(sigs, sk.Sign(message))
	}

	aggSig := AggregateSignatures(sigs)
	valid := FastAggregateVerify(aggSig, pks, message)
	assert.True(t, valid)
}

func TestPublicKeyTo48Bytes(t *testing.T) {
	_, pk, err := GenerateKey()
	require.NoError(t, err)

	bytes48 := pk.To48Bytes()
	assert.Len(t, bytes48, 48)
}

func TestSignatureTo96Bytes(t *testing.T) {
	sk, _, err := GenerateKey()
	require.NoError(t, err)

	sig := sk.Sign([]byte("test"))
	bytes96 := sig.To96Bytes()
	assert.Len(t, bytes96, 96)
}

func TestPubKeyToAddress(t *testing.T) {
	pubKey := []byte("fake public key for testing address derivation")
	addr := PubKeyToAddress(pubKey)

	assert.Len(t, addr, 20)
}

func TestHashToAddress(t *testing.T) {
	hash := Hash([]byte("test"))
	addr := HashToAddress(hash)

	assert.Len(t, addr, 20)
	// Address should be last 20 bytes of hash
	assert.Equal(t, hash[12:], addr[:])
}
