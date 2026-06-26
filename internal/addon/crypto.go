package addon

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

var (
	cryptoKey     []byte
	cryptoKeyOnce sync.Once
)

// initCryptoKey retrieves and prepares the AES-256 symmetric key.
func initCryptoKey() {
	envKey := os.Getenv("EASYNEWS_CRYPTO_KEY")
	if envKey == "" {
		// Generate a secure random 32-byte key for temporary process life
		addonLogger.Warn("--------------------------------------------------------------------------------")
		addonLogger.Warn("EASYNEWS_CRYPTO_KEY environment variable is not set!")
		addonLogger.Warn("A temporary random key has been generated for this run.")
		addonLogger.Warn("CRITICAL: Server restarts will invalidate previously generated encrypted links!")
		addonLogger.Warn("To make links persistent across restarts, set EASYNEWS_CRYPTO_KEY in your env.")
		addonLogger.Warn("--------------------------------------------------------------------------------")

		tempKey := make([]byte, 32)
		if _, err := rand.Read(tempKey); err != nil {
			panic("failed to generate secure random key: " + err.Error())
		}
		cryptoKey = tempKey
		return
	}

	// Derives a stable, cryptographically strong 32-byte key from any user-defined string
	hash := sha256.Sum256([]byte(envKey))
	cryptoKey = hash[:]
}

// GetCryptoKey returns the compiled 32-byte symmetric key.
func GetCryptoKey() []byte {
	cryptoKeyOnce.Do(initCryptoKey)
	return cryptoKey
}

// EncryptConfig encrypts a plaintext configuration string into an URL-safe base64 string.
func EncryptConfig(plaintext string) (string, error) {
	key := GetCryptoKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Generate a unique 12-byte cryptographically secure initialization vector (IV)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Encrypt using authenticated Galois/Counter Mode
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	// Append ciphertext to nonce and encode to raw url-safe base64
	combined := append(nonce, ciphertext...)
	return "enc_" + base64.RawURLEncoding.EncodeToString(combined), nil
}

// DecryptConfig decodes and decrypts an 'enc_' prefixed config string back to plaintext.
func DecryptConfig(encrypted string) (string, error) {
	if !strings.HasPrefix(encrypted, "enc_") {
		return "", fmt.Errorf("invalid encryption prefix")
	}

	trimmed := strings.TrimPrefix(encrypted, "enc_")
	data, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return "", err
	}

	key := GetCryptoKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
