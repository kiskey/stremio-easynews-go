package seal

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "errors"
    "os"
    "strings"
    "sync"

    "github.com/bytedance/sonic"
)

const (
    prefix   = "enc."
    keyLen   = 32 // AES-256
    nonceLen = 12 // GCM standard
    keyID    = '1'
)

var (
    aead    cipher.AEAD
    once    sync.Once
    initErr error
)

// Init initializes the AES cipher block using the ADDON_CONFIG_KEY environment variable.
func Init() error {
    once.Do(func() {
        b64 := os.Getenv("ADDON_CONFIG_KEY")
        if b64 == "" {
            initErr = errors.New("ADDON_CONFIG_KEY not set; falling back to legacy plaintext config")
            return
        }
        k, err := base64.StdEncoding.DecodeString(b64)
        if err != nil || len(k) != keyLen {
            initErr = errors.New("ADDON_CONFIG_KEY must be base64-encoded 32 bytes (AES-256)")
            return
        }
        block, err := aes.NewCipher(k)
        if err != nil {
            initErr = err
            return
        }
        a, err := cipher.NewGCM(block)
        if err != nil {
            initErr = err
            return
        }
        aead = a
    })
    return initErr
}

// Enabled checks if the encryption module is successfully initialized.
func Enabled() bool { return aead != nil }

// IsSealed checks if a string contains the encrypted token prefix.
func IsSealed(s string) bool {
    return strings.HasPrefix(s, prefix)
}

// Seal encrypts plaintext and returns "enc.<keyID>.<base64url(nonce||ct||tag)>".
func Seal(plaintext []byte) (string, error) {
    if aead == nil {
        return "", errors.New("seal not initialized")
    }
    nonce := make([]byte, nonceLen)
    if _, err := rand.Read(nonce); err != nil {
        return "", err
    }
    // Seal appends the 16-byte GCM auth tag to the ciphertext
    ct := aead.Seal(nil, nonce, plaintext, nil)
    blob := append(append([]byte{}, nonce...), ct...)
    return prefix + string(keyID) + "." + base64.RawURLEncoding.EncodeToString(blob), nil
}

// Open decrypts a sealed token string.
func Open(tok string) ([]byte, error) {
    if aead == nil {
        return nil, errors.New("seal not initialized")
    }
    if !IsSealed(tok) {
        return nil, errors.New("not a sealed token")
    }
    
    // Safely extract the base64 payload using string splitting to avoid any index math errors.
    // Expected format: "enc.1.<payload>"
    parts := strings.SplitN(tok, ".", 3)
    if len(parts) != 3 {
        return nil, errors.New("invalid token format")
    }
    if parts[0] != "enc" || parts[1] != string(keyID) {
        return nil, errors.New("unsupported keyID or prefix")
    }
    body := parts[2]
    
    if len(body) == 0 {
        return nil, errors.New("empty token body")
    }
    blob, err := base64.RawURLEncoding.DecodeString(body)
    if err != nil {
        return nil, err
    }
    if len(blob) < nonceLen+16 { // 16 is GCM tag size
        return nil, errors.New("token too short")
    }
    return aead.Open(nil, blob[:nonceLen], blob[nonceLen:], nil)
}

// SealConfig serializes and encrypts an AddonConfig struct.
func SealConfig(c any) (string, error) {
    b, err := sonic.Marshal(c)
    if err != nil {
        return "", err
    }
    return Seal(b)
}

// OpenConfig decrypts and deserializes into an AddonConfig struct.
func OpenConfig(tok string, c any) error {
    b, err := Open(tok)
    if err != nil {
        return err
    }
    return sonic.Unmarshal(b, c)
}
