package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const SERVER_PUBKEY_PEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

type AuthIdentity struct {
	Name               string `json:"name"`
	Aid                string `json:"aid"`
	Uid                string `json:"uid"`
	YxUid              string `json:"yx_uid"`
	OrganizationId     string `json:"organization_id"`
	OrganizationName   string `json:"organization_name"`
	UserType           string `json:"user_type"`
	SecurityOauthToken string `json:"security_oauth_token"`
	RefreshToken       string `json:"refresh_token"`
}

type SessionContext struct {
	TempKey      []byte
	CosyKey      string
	Info         string
	Identity     AuthIdentity
	MachineId    string
	MachineToken string
	MachineType  string
}

func NewSession(identity AuthIdentity, machineId, machineToken, machineType string) (*SessionContext, error) {
	tempKey := []byte(strings.ReplaceAll(generateUUID(), "-", "")[:16])

	cosyKey, err := rsaEncrypt(tempKey)
	if err != nil {
		return nil, err
	}

	authPayload, err := authPayloadJson(identity)
	if err != nil {
		return nil, err
	}

	info, err := aesEncrypt(authPayload, tempKey)
	if err != nil {
		return nil, err
	}

	return &SessionContext{
		TempKey:      tempKey,
		CosyKey:      base64.StdEncoding.EncodeToString(cosyKey),
		Info:         base64.StdEncoding.EncodeToString(info),
		Identity:     identity,
		MachineId:    machineId,
		MachineToken: machineToken,
		MachineType:  machineType,
	}, nil
}

func SignRequest(payloadB64, cosyKey, cosyDate, body, pathWithoutAlgo string) (string, error) {
	s := payloadB64 + "\n" + cosyKey + "\n" + cosyDate + "\n" + body + "\n" + pathWithoutAlgo
	return md5Hash(s), nil
}

const CosyVersionCLI = "1.1.20"

func BuildPayloadB64(info string) (string, error) {
	m := map[string]string{
		"cosyVersion": CosyVersionCLI,
		"ideVersion":  "",
		"info":        info,
		"requestId":   generateUUID(),
		"version":     "v1",
	}

	// Sort keys
	sortedKeys := make([]string, 0, len(m))
	for k := range m {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	sortedMap := make(map[string]string)
	for _, k := range sortedKeys {
		sortedMap[k] = m[k]
	}

	data, err := json.Marshal(sortedMap)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func ComposeBearer(payloadB64, sig string) string {
	return "Bearer COSY." + payloadB64 + "." + sig
}

func authPayloadJson(id AuthIdentity) ([]byte, error) {
	m := map[string]string{
		"name":                 id.Name,
		"aid":                  id.Aid,
		"uid":                  id.Uid,
		"yx_uid":               id.YxUid,
		"organization_id":      id.OrganizationId,
		"organization_name":    id.OrganizationName,
		"user_type":            id.UserType,
		"security_oauth_token": id.SecurityOauthToken,
		"refresh_token":        id.RefreshToken,
	}
	return json.Marshal(m)
}

func rsaEncrypt(tempKey []byte) ([]byte, error) {
	b64 := strings.ReplaceAll(strings.ReplaceAll(SERVER_PUBKEY_PEM, "-----BEGIN PUBLIC KEY-----", ""), "-----END PUBLIC KEY-----", "")
	b64 = strings.ReplaceAll(b64, "\n", "")
	b64 = strings.ReplaceAll(b64, " ", "")

	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}

	pubKey, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}

	return rsa.EncryptPKCS1v15(rand.Reader, rsaPubKey, tempKey)
}

func aesEncrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(plain)%aes.BlockSize != 0 {
		// Pad with PKCS5
		padding := aes.BlockSize - (len(plain) % aes.BlockSize)
		plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	}

	ciphertext := make([]byte, len(plain))
	iv := key[:aes.BlockSize]
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plain)

	return ciphertext, nil
}

func md5Hash(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func generateUUID() string {
	// Simple UUID v4-like generator using crypto/rand
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	// Set version bits for UUID v4
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
