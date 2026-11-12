package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// Cipher 負責對資料庫中的機敏資料（如 SNMP 密碼、AD 繫結密碼、SMTP 密碼）進行 AES-256-GCM 加密。
type Cipher struct {
	key []byte
}

// NewCipher 透過 32 位元組之金鑰（可為 64 碼 Hex 或 32 位元組純文字）建立加密處理器。
func NewCipher(secretKey string) (*Cipher, error) {
	if secretKey == "" {
		return nil, errors.New("加密金鑰未設定 (WURZBURG_SECRET_KEY 不可為空)")
	}

	var key []byte
	if len(secretKey) == 64 {
		decoded, err := hex.DecodeString(secretKey)
		if err == nil && len(decoded) == 32 {
			key = decoded
		}
	}

	if key == nil {
		if len(secretKey) < 32 {
			return nil, fmt.Errorf("加密金鑰長度不足（需至少 32 位元組或 64 位元 Hex 字串，目前為 %d 位元組）", len(secretKey))
		}
		key = []byte(secretKey[:32])
	}

	return &Cipher{key: key}, nil
}

// Encrypt 使用 AES-GCM 將明文字串加密為 Hex 編碼之密文字串。
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("初始化 AES 失敗: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("初始化 GCM 模式失敗: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("產生隨機 Nonce 失敗: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt 將 Hex 編碼之密文還原為明文字串。
func (c *Cipher) Decrypt(encryptedHex string) (string, error) {
	if encryptedHex == "" {
		return "", nil
	}

	data, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", fmt.Errorf("解碼 Hex 密文失敗: %w", err)
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("初始化 AES 失敗: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("初始化 GCM 模式失敗: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("密文資料長度短於 Nonce 規格，內容可能已損壞")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("解密失敗，金鑰可能不正確或密文遭竄改")
	}

	return string(plaintext), nil
}
