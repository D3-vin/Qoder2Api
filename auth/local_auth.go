package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type UserInfo struct {
	// Add fields as needed based on actual JSON structure
}

func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qoder", ".auth")
}

func ReadMachineId() (string, error) {
	data, err := os.ReadFile(filepath.Join(DefaultDir(), "id"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func ReadUserInfo() (map[string]interface{}, error) {
	mid, err := ReadMachineId()
	if err != nil {
		return nil, err
	}

	userData, err := os.ReadFile(filepath.Join(DefaultDir(), "user"))
	if err != nil {
		return nil, err
	}

	cipherBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(userData)))
	if err != nil {
		return nil, err
	}

	key := []byte(mid[:16])

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(cipherBytes) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	iv := key[:aes.BlockSize]
	mode := cipher.NewCBCDecrypter(block, iv)

	plain := make([]byte, len(cipherBytes))
	mode.CryptBlocks(plain, cipherBytes)

	// Remove PKCS5 padding
	padding := int(plain[len(plain)-1])
	plain = plain[:len(plain)-padding]

	var result map[string]interface{}
	if err := json.Unmarshal(plain, &result); err != nil {
		return nil, err
	}

	return result, nil
}
