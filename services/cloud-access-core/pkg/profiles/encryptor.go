package profiles

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/hkdf"
)

// Encryptor transforms bundles into AES-GCM sealed payloads.
type Encryptor struct {
	baseKey []byte
}

// NewEncryptor builds an encryptor with the provided base key (or a derived default).
func NewEncryptor(baseKey []byte) *Encryptor {
	if len(baseKey) == 0 {
		sum := sha256.Sum256([]byte("cloud-access-core-device-secret"))
		baseKey = sum[:]
	}
	return &Encryptor{baseKey: baseKey}
}

// Encrypt seals the bundle with a key derived from the provided token.
func (e *Encryptor) Encrypt(token string, bundle Bundle) (string, error) {
	if token == "" {
		return "", fmt.Errorf("encrypt bundle: token required")
	}
	payload, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("marshal bundle: %w", err)
	}
	block, err := aes.NewCipher(deriveKey(e.baseKey, token))
	if err != nil {
		return "", fmt.Errorf("cipher init: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm init: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, payload, nil)
	result := append(nonce, sealed...)
	return base64.RawStdEncoding.EncodeToString(result), nil
}

func deriveKey(baseKey []byte, token string) []byte {
	h := hkdf.New(sha256.New, []byte(token), baseKey, []byte("device-config"))
	key := make([]byte, 32)
	if _, err := h.Read(key); err != nil {
		sum := sha256.Sum256(append(baseKey, []byte(token)...))
		return sum[:]
	}
	return key
}
