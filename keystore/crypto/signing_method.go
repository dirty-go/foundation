package crypto

import "sync"

var signingMethods = map[string]func() SigningMethod{}
var signingMethodMu = new(sync.RWMutex)

// Implement SigningMethod to add new methods for signing or verifying signed string
type SigningMethod interface {
	Verify(signingString, signature string, key any) error
	Sign(signingString string, key any) (string, error)
	Alg() string
}

func RegisterSigningMethod(alg string, f func() SigningMethod) {
	signingMethodMu.Lock()
	defer signingMethodMu.Unlock()

	signingMethods[alg] = f
}

func GetSigningMethod(alg string) (method SigningMethod) {
	signingMethodMu.RLock()
	defer signingMethodMu.RUnlock()

	if methodF, ok := signingMethods[alg]; ok {
		method = methodF()
	}

	return
}
