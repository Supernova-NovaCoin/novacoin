// Package zk implements zero-knowledge proof components for NovaCoin.
//
// This package provides STARK-friendly primitives including:
//   - Finite field arithmetic over a STARK-friendly prime
//   - Poseidon hash function for efficient in-circuit hashing
//   - Execution trace generation for EVM transactions
//   - Batch proof generation and verification
package zk

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math/big"
)

// Field represents the STARK-friendly finite field.
// We use the BN254 scalar field for compatibility.
// P = 21888242871839275222246405745257275088548364400416034343698204186575808495617

var (
	// Modulus is the field modulus (BN254 scalar field)
	Modulus = mustParseBig("21888242871839275222246405745257275088548364400416034343698204186575808495617")

	// Zero is the zero element
	Zero = NewFieldElement(0)

	// One is the one element
	One = NewFieldElement(1)

	// Generator is a generator of the multiplicative group
	Generator = NewFieldElementFromBig(big.NewInt(5))
)

// FieldElement represents an element in the finite field.
type FieldElement struct {
	value *big.Int
}

// NewFieldElement creates a new field element from uint64.
func NewFieldElement(v uint64) FieldElement {
	return FieldElement{value: new(big.Int).SetUint64(v)}
}

// NewFieldElementFromBig creates a new field element from big.Int.
func NewFieldElementFromBig(v *big.Int) FieldElement {
	result := new(big.Int).Set(v)
	result.Mod(result, Modulus)
	return FieldElement{value: result}
}

// NewFieldElementFromBytes creates a field element from bytes.
func NewFieldElementFromBytes(b []byte) FieldElement {
	v := new(big.Int).SetBytes(b)
	v.Mod(v, Modulus)
	return FieldElement{value: v}
}

// RandomFieldElement generates a random field element.
func RandomFieldElement() (FieldElement, error) {
	max := new(big.Int).Sub(Modulus, big.NewInt(1))
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return FieldElement{}, err
	}
	return FieldElement{value: n}, nil
}

// Add returns a + b mod p.
func (a FieldElement) Add(b FieldElement) FieldElement {
	av := a.value
	bv := b.value
	if av == nil {
		av = big.NewInt(0)
	}
	if bv == nil {
		bv = big.NewInt(0)
	}
	result := new(big.Int).Add(av, bv)
	result.Mod(result, Modulus)
	return FieldElement{value: result}
}

// Sub returns a - b mod p.
func (a FieldElement) Sub(b FieldElement) FieldElement {
	av := a.value
	bv := b.value
	if av == nil {
		av = big.NewInt(0)
	}
	if bv == nil {
		bv = big.NewInt(0)
	}
	result := new(big.Int).Sub(av, bv)
	result.Mod(result, Modulus)
	if result.Sign() < 0 {
		result.Add(result, Modulus)
	}
	return FieldElement{value: result}
}

// Mul returns a * b mod p.
func (a FieldElement) Mul(b FieldElement) FieldElement {
	av := a.value
	bv := b.value
	if av == nil {
		av = big.NewInt(0)
	}
	if bv == nil {
		bv = big.NewInt(0)
	}
	result := new(big.Int).Mul(av, bv)
	result.Mod(result, Modulus)
	return FieldElement{value: result}
}

// Div returns a / b mod p (a * b^(-1)).
func (a FieldElement) Div(b FieldElement) (FieldElement, error) {
	if b.IsZero() {
		return FieldElement{}, errors.New("division by zero")
	}
	inv := new(big.Int).ModInverse(b.value, Modulus)
	result := new(big.Int).Mul(a.value, inv)
	result.Mod(result, Modulus)
	return FieldElement{value: result}, nil
}

// Neg returns -a mod p.
func (a FieldElement) Neg() FieldElement {
	result := new(big.Int).Neg(a.value)
	result.Mod(result, Modulus)
	if result.Sign() < 0 {
		result.Add(result, Modulus)
	}
	return FieldElement{value: result}
}

// Inv returns a^(-1) mod p.
func (a FieldElement) Inv() (FieldElement, error) {
	if a.IsZero() {
		return FieldElement{}, errors.New("inverse of zero")
	}
	result := new(big.Int).ModInverse(a.value, Modulus)
	return FieldElement{value: result}, nil
}

// Pow returns a^exp mod p.
func (a FieldElement) Pow(exp *big.Int) FieldElement {
	result := new(big.Int).Exp(a.value, exp, Modulus)
	return FieldElement{value: result}
}

// PowUint64 returns a^exp mod p for uint64 exponent.
func (a FieldElement) PowUint64(exp uint64) FieldElement {
	return a.Pow(new(big.Int).SetUint64(exp))
}

// Square returns a^2 mod p.
func (a FieldElement) Square() FieldElement {
	return a.Mul(a)
}

// Sqrt returns the square root of a if it exists.
func (a FieldElement) Sqrt() (FieldElement, bool) {
	// Using Tonelli-Shanks algorithm
	// For BN254, p ≡ 3 (mod 4), so sqrt = a^((p+1)/4)
	exp := new(big.Int).Add(Modulus, big.NewInt(1))
	exp.Rsh(exp, 2)
	result := a.Pow(exp)

	// Verify
	if result.Square().Equal(a) {
		return result, true
	}
	return FieldElement{}, false
}

// IsZero returns true if a == 0.
func (a FieldElement) IsZero() bool {
	if a.value == nil {
		return true
	}
	return a.value.Sign() == 0
}

// Equal returns true if a == b.
func (a FieldElement) Equal(b FieldElement) bool {
	av := a.value
	bv := b.value
	if av == nil {
		av = big.NewInt(0)
	}
	if bv == nil {
		bv = big.NewInt(0)
	}
	return av.Cmp(bv) == 0
}

// Cmp compares a and b.
func (a FieldElement) Cmp(b FieldElement) int {
	av := a.value
	bv := b.value
	if av == nil {
		av = big.NewInt(0)
	}
	if bv == nil {
		bv = big.NewInt(0)
	}
	return av.Cmp(bv)
}

// Bytes returns the big-endian byte representation.
func (a FieldElement) Bytes() []byte {
	v := a.value
	if v == nil {
		v = big.NewInt(0)
	}
	b := v.Bytes()
	// Pad to 32 bytes
	if len(b) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		return padded
	}
	return b
}

// Uint64 returns the uint64 representation (truncated).
func (a FieldElement) Uint64() uint64 {
	if a.value == nil {
		return 0
	}
	return a.value.Uint64()
}

// BigInt returns the big.Int value.
func (a FieldElement) BigInt() *big.Int {
	if a.value == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(a.value)
}

// String returns the string representation.
func (a FieldElement) String() string {
	if a.value == nil {
		return "0"
	}
	return a.value.String()
}

// Copy returns a copy of the field element.
func (a FieldElement) Copy() FieldElement {
	if a.value == nil {
		return FieldElement{value: big.NewInt(0)}
	}
	return FieldElement{value: new(big.Int).Set(a.value)}
}

// === Polynomial Operations ===

// Polynomial represents a polynomial over the finite field.
type Polynomial struct {
	Coeffs []FieldElement
}

// NewPolynomial creates a polynomial from coefficients.
// coeffs[i] is the coefficient of x^i.
func NewPolynomial(coeffs []FieldElement) *Polynomial {
	return &Polynomial{Coeffs: coeffs}
}

// Degree returns the degree of the polynomial.
func (p *Polynomial) Degree() int {
	return len(p.Coeffs) - 1
}

// Evaluate evaluates the polynomial at point x.
func (p *Polynomial) Evaluate(x FieldElement) FieldElement {
	if len(p.Coeffs) == 0 {
		return Zero
	}

	// Horner's method
	result := p.Coeffs[len(p.Coeffs)-1].Copy()
	for i := len(p.Coeffs) - 2; i >= 0; i-- {
		result = result.Mul(x).Add(p.Coeffs[i])
	}
	return result
}

// Add adds two polynomials.
func (p *Polynomial) Add(q *Polynomial) *Polynomial {
	maxLen := len(p.Coeffs)
	if len(q.Coeffs) > maxLen {
		maxLen = len(q.Coeffs)
	}

	result := make([]FieldElement, maxLen)
	for i := 0; i < maxLen; i++ {
		var a, b FieldElement
		if i < len(p.Coeffs) {
			a = p.Coeffs[i]
		} else {
			a = Zero
		}
		if i < len(q.Coeffs) {
			b = q.Coeffs[i]
		} else {
			b = Zero
		}
		result[i] = a.Add(b)
	}

	return &Polynomial{Coeffs: result}
}

// Mul multiplies two polynomials.
func (p *Polynomial) Mul(q *Polynomial) *Polynomial {
	if len(p.Coeffs) == 0 || len(q.Coeffs) == 0 {
		return &Polynomial{}
	}

	result := make([]FieldElement, len(p.Coeffs)+len(q.Coeffs)-1)
	for i := range result {
		result[i] = Zero
	}

	for i, a := range p.Coeffs {
		for j, b := range q.Coeffs {
			result[i+j] = result[i+j].Add(a.Mul(b))
		}
	}

	return &Polynomial{Coeffs: result}
}

// Interpolate performs Lagrange interpolation.
func Interpolate(points []FieldElement, values []FieldElement) (*Polynomial, error) {
	if len(points) != len(values) {
		return nil, errors.New("points and values must have same length")
	}
	if len(points) == 0 {
		return &Polynomial{Coeffs: []FieldElement{Zero}}, nil
	}
	if len(points) == 1 {
		return &Polynomial{Coeffs: []FieldElement{values[0].Copy()}}, nil
	}

	n := len(points)
	result := make([]FieldElement, n)
	for i := range result {
		result[i] = Zero
	}

	for i := 0; i < n; i++ {
		// Compute Lagrange basis polynomial L_i(x)
		basis := []FieldElement{One}

		for j := 0; j < n; j++ {
			if i == j {
				continue
			}

			// Check for duplicate points
			denom := points[i].Sub(points[j])
			if denom.IsZero() {
				// Skip duplicate points
				continue
			}

			denomInv, err := denom.Inv()
			if err != nil {
				return nil, err
			}

			// New coefficients: multiply by (x - x_j) * denomInv
			newBasis := make([]FieldElement, len(basis)+1)
			for k := range newBasis {
				newBasis[k] = Zero
			}

			for k, c := range basis {
				if c.value == nil {
					continue
				}
				// c * x term
				term1 := c.Mul(denomInv)
				newBasis[k+1] = newBasis[k+1].Add(term1)
				// c * (-x_j) term
				term2 := c.Mul(points[j].Neg()).Mul(denomInv)
				newBasis[k] = newBasis[k].Add(term2)
			}
			basis = newBasis
		}

		// Add y_i * L_i(x) to result
		for k, c := range basis {
			if k < len(result) && c.value != nil && values[i].value != nil {
				result[k] = result[k].Add(values[i].Mul(c))
			}
		}
	}

	return &Polynomial{Coeffs: result}, nil
}

// === Merkle Tree ===

// MerkleTree is a binary Merkle tree for commitment.
type MerkleTree struct {
	Leaves []FieldElement
	Nodes  [][]FieldElement
	Root   FieldElement
}

// NewMerkleTree builds a Merkle tree from leaves.
func NewMerkleTree(leaves []FieldElement) *MerkleTree {
	if len(leaves) == 0 {
		return &MerkleTree{}
	}

	// Pad to power of 2
	n := 1
	for n < len(leaves) {
		n *= 2
	}
	paddedLeaves := make([]FieldElement, n)
	copy(paddedLeaves, leaves)
	for i := len(leaves); i < n; i++ {
		paddedLeaves[i] = Zero
	}

	// Build tree
	levels := [][]FieldElement{paddedLeaves}
	current := paddedLeaves

	for len(current) > 1 {
		next := make([]FieldElement, len(current)/2)
		for i := 0; i < len(current)/2; i++ {
			next[i] = HashTwo(current[2*i], current[2*i+1])
		}
		levels = append(levels, next)
		current = next
	}

	return &MerkleTree{
		Leaves: leaves,
		Nodes:  levels,
		Root:   current[0],
	}
}

// GetProof returns the Merkle proof for leaf at index.
func (mt *MerkleTree) GetProof(index int) []FieldElement {
	if index >= len(mt.Leaves) {
		return nil
	}

	proof := make([]FieldElement, 0)
	idx := index

	for level := 0; level < len(mt.Nodes)-1; level++ {
		if idx%2 == 0 {
			proof = append(proof, mt.Nodes[level][idx+1])
		} else {
			proof = append(proof, mt.Nodes[level][idx-1])
		}
		idx /= 2
	}

	return proof
}

// VerifyProof verifies a Merkle proof.
func VerifyProof(root, leaf FieldElement, index int, proof []FieldElement) bool {
	current := leaf
	idx := index

	for _, sibling := range proof {
		if idx%2 == 0 {
			current = HashTwo(current, sibling)
		} else {
			current = HashTwo(sibling, current)
		}
		idx /= 2
	}

	return current.Equal(root)
}

// HashTwo computes a 2-to-1 hash using simplified Poseidon.
func HashTwo(a, b FieldElement) FieldElement {
	// Simplified Poseidon-like hash (not cryptographically secure, for demonstration)
	// Real implementation would use proper Poseidon constants
	state := a.Add(b.Mul(NewFieldElement(2)))
	state = state.PowUint64(5).Add(a).Add(b)
	return state
}

// === Helper Functions ===

func mustParseBig(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("invalid big integer: " + s)
	}
	return n
}

// FieldElementsToBytes converts field elements to bytes.
func FieldElementsToBytes(elements []FieldElement) []byte {
	result := make([]byte, len(elements)*32)
	for i, e := range elements {
		copy(result[i*32:], e.Bytes())
	}
	return result
}

// BytesToFieldElements converts bytes to field elements.
func BytesToFieldElements(data []byte) []FieldElement {
	n := (len(data) + 31) / 32
	result := make([]FieldElement, n)
	for i := 0; i < n; i++ {
		start := i * 32
		end := start + 32
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, 32)
		copy(chunk[32-(end-start):], data[start:end])
		result[i] = NewFieldElementFromBytes(chunk)
	}
	return result
}

// Uint64ToFieldElement converts uint64 to field element.
func Uint64ToFieldElement(v uint64) FieldElement {
	return NewFieldElement(v)
}

// Uint256ToFieldElements splits a 256-bit value into field elements.
func Uint256ToFieldElements(b []byte) []FieldElement {
	if len(b) != 32 {
		b = append(make([]byte, 32-len(b)), b...)
	}
	// Split into two 128-bit values for field representation
	high := NewFieldElementFromBytes(b[:16])
	low := NewFieldElementFromBytes(b[16:])
	return []FieldElement{high, low}
}

// EncodeBytesToField encodes arbitrary bytes to field elements.
func EncodeBytesToField(data []byte) []FieldElement {
	// Encode length first
	result := []FieldElement{NewFieldElement(uint64(len(data)))}
	// Then encode data in 31-byte chunks (to stay within field)
	for i := 0; i < len(data); i += 31 {
		end := i + 31
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, 32)
		copy(chunk[32-(end-i):], data[i:end])
		result = append(result, NewFieldElementFromBytes(chunk))
	}
	return result
}

// RootsOfUnity generates n-th roots of unity.
func RootsOfUnity(n int) []FieldElement {
	if n == 0 {
		return nil
	}

	// Find primitive n-th root of unity
	// For BN254, group order is Modulus - 1
	// omega = Generator^((Modulus-1)/n)
	exp := new(big.Int).Sub(Modulus, big.NewInt(1))
	exp.Div(exp, big.NewInt(int64(n)))
	omega := Generator.Pow(exp)

	roots := make([]FieldElement, n)
	roots[0] = One
	for i := 1; i < n; i++ {
		roots[i] = roots[i-1].Mul(omega)
	}
	return roots
}

// FFT performs Fast Fourier Transform over the field.
func FFT(coeffs []FieldElement) []FieldElement {
	n := len(coeffs)
	if n == 1 {
		return coeffs
	}

	// Ensure n is power of 2
	if n&(n-1) != 0 {
		return nil // n must be power of 2
	}

	roots := RootsOfUnity(n)
	return fftCore(coeffs, roots)
}

func fftCore(coeffs []FieldElement, roots []FieldElement) []FieldElement {
	n := len(coeffs)
	if n == 1 {
		return coeffs
	}

	// Split into even and odd
	even := make([]FieldElement, n/2)
	odd := make([]FieldElement, n/2)
	evenRoots := make([]FieldElement, n/2)
	for i := 0; i < n/2; i++ {
		even[i] = coeffs[2*i]
		odd[i] = coeffs[2*i+1]
		evenRoots[i] = roots[2*i]
	}

	// Recursive FFT
	evenFFT := fftCore(even, evenRoots)
	oddFFT := fftCore(odd, evenRoots)

	// Combine
	result := make([]FieldElement, n)
	for i := 0; i < n/2; i++ {
		t := roots[i].Mul(oddFFT[i])
		result[i] = evenFFT[i].Add(t)
		result[i+n/2] = evenFFT[i].Sub(t)
	}

	return result
}

// RandomBytes generates n random bytes.
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// BytesToUint64 converts bytes to uint64.
func BytesToUint64(b []byte) uint64 {
	if len(b) < 8 {
		padded := make([]byte, 8)
		copy(padded[8-len(b):], b)
		b = padded
	}
	return binary.BigEndian.Uint64(b[:8])
}
