package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"

	"github.com/timemore/foundation/errors"
)

type SigningMethodRSA struct {
	Name string
	Hash crypto.Hash
}

// Specific instances for RSA256 and company
var (
	SigningMethodRS256 *SigningMethodRSA
	SigningMethodRS384 *SigningMethodRSA
	SigningMethodRS512 *SigningMethodRSA
)

func init() {
	// RS256
	SigningMethodRS256 = &SigningMethodRSA{"RS256", crypto.SHA256}
	RegisterSigningMethod(SigningMethodRS256.Alg(), func() SigningMethod {
		return SigningMethodRS256
	})

	// RS384
	SigningMethodRS384 = &SigningMethodRSA{"RS384", crypto.SHA384}
	RegisterSigningMethod(SigningMethodRS384.Alg(), func() SigningMethod {
		return SigningMethodRS384
	})

	// RS512
	SigningMethodRS512 = &SigningMethodRSA{"RS512", crypto.SHA512}
	RegisterSigningMethod(SigningMethodRS512.Alg(), func() SigningMethod {
		return SigningMethodRS512
	})
}

var _ SigningMethod = &SigningMethodRSA{}

func (m *SigningMethodRSA) Alg() string {
	return m.Name
}

// Verify Implement the Verify method from SigningMethod
// For this signing method, must be *rsa.PublicKey structure
func (m *SigningMethodRSA) Verify(signingString, signature string, key any) error {
	var err error

	// Decode the signature
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return errors.Wrap("decoding signature", err)
	}

	var rsaKey *rsa.PublicKey
	var ok bool
	if rsaKey, ok = key.(*rsa.PublicKey); !ok {
		return ErrInvalidKeyType
	}

	// Create hasher
	if !m.Hash.Available() {
		return ErrHashUnavailable
	}
	hasher := m.Hash.New()
	hasher.Write([]byte(signingString))

	// verify the signature
	return rsa.VerifyPKCS1v15(rsaKey, m.Hash, hasher.Sum(nil), sig)
}

// Sign Implements the sign method from SigningMethod
// For this signing method, must be an *rsa.PrivateKey structure
func (m *SigningMethodRSA) Sign(text string, key any) (string, error) {
	var rsaKey *rsa.PrivateKey
	var ok bool
	if rsaKey, ok = key.(*rsa.PrivateKey); !ok {
		return "", ErrInvalidKey
	}

	// Create hasher
	if !m.Hash.Available() {
		return "", ErrHashUnavailable
	}

	hasher := m.Hash.New()
	hasher.Write([]byte(text))

	// Sign the text and return the encoded bytes
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, m.Hash, hasher.Sum(nil))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(sigBytes), nil
}
