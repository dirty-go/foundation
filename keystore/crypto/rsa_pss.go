package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"

	"github.com/timemore/foundation/errors"
)

type SigningMethodRSAPSS struct {
	*SigningMethodRSA

	Options *rsa.PSSOptions
	// VerifyOptions is optional, If set overrides Options for rsa.VerifyPPS.
	// Used to accept tokens signed with rsa.PSSSaltLengthAuto, what doesn't follow
	// https://tools.ietf.org/html/rfc7518#section-3.5 but was used previously.
	// See https://github.com/dgrijalva/jwt-go/issues/285#issuecomment-437451244 for details.
	VerifyOptions *rsa.PSSOptions
}

// Specific instances for RS/PS and company.
var (
	SigningMethodPS256 *SigningMethodRSAPSS
	SigningMethodPS384 *SigningMethodRSAPSS
	SigningMethodPS512 *SigningMethodRSAPSS
)

func init() {
	// PS256
	SigningMethodPS256 = &SigningMethodRSAPSS{
		SigningMethodRSA: &SigningMethodRSA{
			Name: "PS256",
			Hash: crypto.SHA256,
		},
		Options: &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthEqualsHash,
		},
		VerifyOptions: &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthAuto,
		},
	}
	RegisterSigningMethod(SigningMethodPS256.Alg(), func() SigningMethod {
		return SigningMethodPS256
	})

	// PS384
	SigningMethodPS384 = &SigningMethodRSAPSS{
		SigningMethodRSA: &SigningMethodRSA{
			Name: "PS384",
			Hash: crypto.SHA384,
		},
		Options: &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthEqualsHash,
		},
		VerifyOptions: &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthAuto,
		},
	}
	RegisterSigningMethod(SigningMethodPS384.Alg(), func() SigningMethod {
		return SigningMethodPS384
	})

	// PS512
	SigningMethodPS512 = &SigningMethodRSAPSS{
		SigningMethodRSA: &SigningMethodRSA{
			Name: "PS512",
			Hash: crypto.SHA512,
		},
		Options: &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthEqualsHash,
		},
		VerifyOptions: &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthAuto,
		},
	}
	RegisterSigningMethod(SigningMethodPS512.Alg(), func() SigningMethod {
		return SigningMethodPS512
	})
}

var _ SigningMethod = &SigningMethodRSAPSS{}

func (m *SigningMethodRSAPSS) Alg() string {
	return m.Name
}

// Implements the Sign method from SigningMethod
// For this signing method, key must be an rsa.PrivateKey struct
func (m *SigningMethodRSAPSS) Sign(text string, key any) (string, error) {
	// Get private key
	var rsaKey *rsa.PrivateKey
	var ok bool
	if rsaKey, ok = key.(*rsa.PrivateKey); !ok {
		return "", ErrInvalidKeyType
	}

	// Create the hasher
	if !m.Hash.Available() {
		return "", ErrHashUnavailable
	}

	hasher := m.Hash.New()
	hasher.Write([]byte(text))

	// Sign the string and return the encoded bytes
	sigBytes, err := rsa.SignPSS(rand.Reader, rsaKey, m.Hash, hasher.Sum(nil), m.Options)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sigBytes), nil
}

// Implements the Verify method from SigningMethod
// For this verify method, key must be an rsa.PublicKey struct
func (m *SigningMethodRSAPSS) Verify(signingString string, signature string, key any) error {
	// Decode the signature
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return errors.Wrap("decode signature", err)
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

	opts := m.Options
	if m.VerifyOptions != nil {
		opts = m.VerifyOptions
	}

	return rsa.VerifyPSS(rsaKey, m.Hash, hasher.Sum(nil), sig, opts)
}
