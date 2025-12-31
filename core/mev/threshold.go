// Package mev implements MEV (Maximal Extractable Value) resistance for NovaCoin.
package mev

import (
	"crypto/rand"
	"errors"
	"math/big"
	"sync"
)

// ThresholdConfig holds threshold encryption configuration.
type ThresholdConfig struct {
	TotalShares    int // Total number of shares (n)
	Threshold      int // Minimum shares needed (k)
	FieldPrime     *big.Int
}

// DefaultThresholdConfig returns default configuration (67-of-100).
func DefaultThresholdConfig() *ThresholdConfig {
	// Use a 256-bit prime for the finite field
	prime, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
	return &ThresholdConfig{
		TotalShares: 100,
		Threshold:   67,
		FieldPrime:  prime,
	}
}

// Share represents a secret share.
type Share struct {
	Index uint32   // Share index (1 to n)
	Value *big.Int // Share value
}

// ShareSet holds a collection of shares for reconstruction.
type ShareSet struct {
	Shares    []*Share
	Threshold int
}

// SecretSharer implements Shamir's Secret Sharing.
type SecretSharer struct {
	config *ThresholdConfig
	mu     sync.RWMutex
}

// NewSecretSharer creates a new secret sharer.
func NewSecretSharer(config *ThresholdConfig) *SecretSharer {
	if config == nil {
		config = DefaultThresholdConfig()
	}
	return &SecretSharer{
		config: config,
	}
}

// Split splits a secret into n shares, requiring k to reconstruct.
func (ss *SecretSharer) Split(secret []byte) ([]*Share, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if len(secret) > 32 {
		return nil, ErrSecretTooLarge
	}

	// Convert secret to big.Int
	secretInt := new(big.Int).SetBytes(secret)
	if secretInt.Cmp(ss.config.FieldPrime) >= 0 {
		return nil, ErrSecretTooLarge
	}

	// Generate random polynomial coefficients
	// f(x) = secret + a1*x + a2*x^2 + ... + a(k-1)*x^(k-1)
	coefficients := make([]*big.Int, ss.config.Threshold)
	coefficients[0] = secretInt

	for i := 1; i < ss.config.Threshold; i++ {
		coef, err := rand.Int(rand.Reader, ss.config.FieldPrime)
		if err != nil {
			return nil, err
		}
		coefficients[i] = coef
	}

	// Evaluate polynomial at points 1 to n
	shares := make([]*Share, ss.config.TotalShares)
	for i := 1; i <= ss.config.TotalShares; i++ {
		x := big.NewInt(int64(i))
		y := ss.evaluatePolynomial(coefficients, x)
		shares[i-1] = &Share{
			Index: uint32(i),
			Value: y,
		}
	}

	return shares, nil
}

// evaluatePolynomial evaluates the polynomial at point x.
func (ss *SecretSharer) evaluatePolynomial(coefficients []*big.Int, x *big.Int) *big.Int {
	result := new(big.Int).Set(coefficients[0])
	xPower := new(big.Int).Set(x)

	for i := 1; i < len(coefficients); i++ {
		term := new(big.Int).Mul(coefficients[i], xPower)
		term.Mod(term, ss.config.FieldPrime)
		result.Add(result, term)
		result.Mod(result, ss.config.FieldPrime)
		xPower.Mul(xPower, x)
		xPower.Mod(xPower, ss.config.FieldPrime)
	}

	return result
}

// Reconstruct reconstructs the secret from shares using Lagrange interpolation.
func (ss *SecretSharer) Reconstruct(shares []*Share) ([]byte, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if len(shares) < ss.config.Threshold {
		return nil, ErrInsufficientShares
	}

	// Use only the first k shares
	shares = shares[:ss.config.Threshold]

	// Lagrange interpolation at x=0
	secret := big.NewInt(0)

	for i := 0; i < len(shares); i++ {
		// Calculate Lagrange basis polynomial L_i(0)
		numerator := big.NewInt(1)
		denominator := big.NewInt(1)

		xi := big.NewInt(int64(shares[i].Index))

		for j := 0; j < len(shares); j++ {
			if i == j {
				continue
			}
			xj := big.NewInt(int64(shares[j].Index))

			// numerator *= -xj (mod prime)
			negXj := new(big.Int).Sub(ss.config.FieldPrime, xj)
			numerator.Mul(numerator, negXj)
			numerator.Mod(numerator, ss.config.FieldPrime)

			// denominator *= (xi - xj) (mod prime)
			diff := new(big.Int).Sub(xi, xj)
			diff.Mod(diff, ss.config.FieldPrime)
			denominator.Mul(denominator, diff)
			denominator.Mod(denominator, ss.config.FieldPrime)
		}

		// Calculate L_i(0) = numerator / denominator (mod prime)
		denominatorInv := new(big.Int).ModInverse(denominator, ss.config.FieldPrime)
		if denominatorInv == nil {
			return nil, ErrReconstructionFailed
		}

		lagrange := new(big.Int).Mul(numerator, denominatorInv)
		lagrange.Mod(lagrange, ss.config.FieldPrime)

		// secret += share_i.value * L_i(0)
		term := new(big.Int).Mul(shares[i].Value, lagrange)
		term.Mod(term, ss.config.FieldPrime)
		secret.Add(secret, term)
		secret.Mod(secret, ss.config.FieldPrime)
	}

	// Convert back to bytes (32 bytes)
	result := make([]byte, 32)
	secretBytes := secret.Bytes()
	copy(result[32-len(secretBytes):], secretBytes)

	return result, nil
}

// VerifyShare verifies that a share is valid for the given commitments.
// Uses Feldman's verifiable secret sharing.
func (ss *SecretSharer) VerifyShare(share *Share, commitments []*big.Int, generator *big.Int) bool {
	if len(commitments) != ss.config.Threshold {
		return false
	}

	// g^share should equal C_0 * C_1^i * C_2^(i^2) * ... * C_(k-1)^(i^(k-1))
	x := big.NewInt(int64(share.Index))
	xPower := big.NewInt(1)

	expected := big.NewInt(1)
	for _, c := range commitments {
		term := new(big.Int).Exp(c, xPower, ss.config.FieldPrime)
		expected.Mul(expected, term)
		expected.Mod(expected, ss.config.FieldPrime)

		xPower.Mul(xPower, x)
		xPower.Mod(xPower, ss.config.FieldPrime)
	}

	actual := new(big.Int).Exp(generator, share.Value, ss.config.FieldPrime)

	return expected.Cmp(actual) == 0
}

// ThresholdKeyManager manages threshold key distribution among validators.
type ThresholdKeyManager struct {
	config  *ThresholdConfig
	sharer  *SecretSharer

	// Active key shares for current epoch
	activeShares map[uint32]*Share // validatorIndex -> share

	// Collected decryption shares for pending blocks
	pendingDecryptions map[[32]byte]*DecryptionSession

	mu sync.RWMutex
}

// DecryptionSession tracks the decryption process for a block.
type DecryptionSession struct {
	BlockHash    [32]byte
	Shares       []*Share
	ShareCount   int
	Threshold    int
	Complete     bool
	DecryptedKey []byte
}

// NewThresholdKeyManager creates a new threshold key manager.
func NewThresholdKeyManager(config *ThresholdConfig) *ThresholdKeyManager {
	if config == nil {
		config = DefaultThresholdConfig()
	}
	return &ThresholdKeyManager{
		config:             config,
		sharer:             NewSecretSharer(config),
		activeShares:       make(map[uint32]*Share),
		pendingDecryptions: make(map[[32]byte]*DecryptionSession),
	}
}

// GenerateEpochKey generates a new encryption key and distributes shares.
func (tkm *ThresholdKeyManager) GenerateEpochKey() ([]byte, []*Share, error) {
	tkm.mu.Lock()
	defer tkm.mu.Unlock()

	// Generate random 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, err
	}

	// Split into shares
	shares, err := tkm.sharer.Split(key)
	if err != nil {
		return nil, nil, err
	}

	return key, shares, nil
}

// DistributeShare assigns a share to a validator.
func (tkm *ThresholdKeyManager) DistributeShare(validatorIndex uint32, share *Share) {
	tkm.mu.Lock()
	defer tkm.mu.Unlock()
	tkm.activeShares[validatorIndex] = share
}

// GetShare returns the share for a validator.
func (tkm *ThresholdKeyManager) GetShare(validatorIndex uint32) *Share {
	tkm.mu.RLock()
	defer tkm.mu.RUnlock()
	return tkm.activeShares[validatorIndex]
}

// StartDecryption initiates a decryption session for a block.
func (tkm *ThresholdKeyManager) StartDecryption(blockHash [32]byte) *DecryptionSession {
	tkm.mu.Lock()
	defer tkm.mu.Unlock()

	session := &DecryptionSession{
		BlockHash:  blockHash,
		Shares:     make([]*Share, 0),
		Threshold:  tkm.config.Threshold,
	}
	tkm.pendingDecryptions[blockHash] = session
	return session
}

// SubmitDecryptionShare submits a decryption share for a block.
func (tkm *ThresholdKeyManager) SubmitDecryptionShare(blockHash [32]byte, share *Share) (bool, []byte, error) {
	tkm.mu.Lock()
	defer tkm.mu.Unlock()

	session, exists := tkm.pendingDecryptions[blockHash]
	if !exists {
		return false, nil, ErrDecryptionNotStarted
	}

	if session.Complete {
		return true, session.DecryptedKey, nil
	}

	// Check for duplicate share
	for _, s := range session.Shares {
		if s.Index == share.Index {
			return false, nil, ErrDuplicateShare
		}
	}

	session.Shares = append(session.Shares, share)
	session.ShareCount++

	// Check if we have enough shares
	if session.ShareCount >= session.Threshold {
		key, err := tkm.sharer.Reconstruct(session.Shares)
		if err != nil {
			return false, nil, err
		}
		session.Complete = true
		session.DecryptedKey = key
		return true, key, nil
	}

	return false, nil, nil
}

// GetDecryptionSession returns a decryption session.
func (tkm *ThresholdKeyManager) GetDecryptionSession(blockHash [32]byte) *DecryptionSession {
	tkm.mu.RLock()
	defer tkm.mu.RUnlock()
	return tkm.pendingDecryptions[blockHash]
}

// CleanupSession removes a completed decryption session.
func (tkm *ThresholdKeyManager) CleanupSession(blockHash [32]byte) {
	tkm.mu.Lock()
	defer tkm.mu.Unlock()
	delete(tkm.pendingDecryptions, blockHash)
}

// Error types
var (
	ErrSecretTooLarge       = errors.New("secret too large for field")
	ErrInsufficientShares   = errors.New("insufficient shares for reconstruction")
	ErrReconstructionFailed = errors.New("failed to reconstruct secret")
	ErrDecryptionNotStarted = errors.New("decryption session not started")
	ErrDuplicateShare       = errors.New("duplicate share submitted")
)
