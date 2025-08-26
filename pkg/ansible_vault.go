package pkg

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// AnsibleVaultString is a struct that represents an Ansible vault string,
// like $ANSIBLE_VAULT;1.2;AES256;some-vault-id
// 623133...
type AnsibleVaultString struct {
	FormatId   string `yaml:"format_id"` // Only valid format so far is "$ANSIBLE_VAULT"
	Version    string `yaml:"version"`
	VaultId    string `yaml:"vault_id"` // Version 1.2 only, optionally
	CipherText string `yaml:"ciphertext"`
}

func IsAnsibleVaultString(yamlContent string) bool {
	// Check for both the YAML tag format and the actual Ansible vault header
	return strings.HasPrefix(yamlContent, "!vault |") || strings.HasPrefix(yamlContent, "$ANSIBLE_VAULT;")
}

func NewAnsibleVaultString(yamlContent string) (*AnsibleVaultString, error) {
	if !IsAnsibleVaultString(yamlContent) {
		return nil, fmt.Errorf("not a valid Ansible vault string")
	}

	// Split into lines
	lines := strings.Split(yamlContent, "\n")

	// Determine the starting line based on format
	var startLine int
	if strings.HasPrefix(yamlContent, "!vault |") {
		// YAML format: !vault | followed by content
		startLine = 1
	} else if strings.HasPrefix(yamlContent, "$ANSIBLE_VAULT;") {
		// Direct vault format: starts with header
		startLine = 0
	} else {
		return nil, fmt.Errorf("unexpected vault format")
	}

	if len(lines) <= startLine {
		return nil, fmt.Errorf("missing header line")
	}

	// Get header line and trim whitespace
	headerLine := strings.TrimSpace(lines[startLine])
	headerParts := strings.Split(headerLine, ";")
	if len(headerParts) < 3 {
		return nil, fmt.Errorf("missing header parts, found %d parts", len(headerParts))
	}

	// Join remaining lines to get ciphertext
	ciphertext := strings.Join(lines[startLine+1:], "")
	ciphertext = strings.TrimSpace(ciphertext)

	// Remove all whitespace and newlines for the old format
	ciphertext = strings.ReplaceAll(ciphertext, "\n", "")
	ciphertext = strings.ReplaceAll(ciphertext, " ", "")

	vault := &AnsibleVaultString{
		FormatId:   headerParts[0],
		Version:    headerParts[1],
		CipherText: ciphertext,
	}

	// Parse vault ID if present (4th part)
	if len(headerParts) > 3 {
		vault.VaultId = headerParts[3]
	}

	return vault, nil
}

func Encrypt(plaintext, password string) (*AnsibleVaultString, error) {
	// Ansible Vault uses AES-256-CTR encryption with PBKDF2 key derivation
	// Reference: https://docs.ansible.com/ansible/2.8/user_guide/vault.html#vault-payload-format-1-1-1-2

	// Generate random salt (32 bytes as per Ansible)
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %v", err)
	}

	// Derive keys using PBKDF2 (10000 iterations as per Ansible)
	// First 32 bytes are cipher key, second 32 bytes are HMAC key, remaining 16 bytes are IV
	derivedKey := pbkdf2.Key([]byte(password), salt, 10000, 80, sha256.New)
	cipherKey := derivedKey[:32]
	hmacKey := derivedKey[32:64]
	iv := derivedKey[64:80]

	// Create AES cipher for CTR mode
	block, err := aes.NewCipher(cipherKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	// Create CTR mode
	ctr := cipher.NewCTR(block, iv)

	// PKCS#7 pad plaintext to AES block size per RFC5652
	pt := []byte(plaintext)
	padLen := aes.BlockSize - (len(pt) % aes.BlockSize)
	if padLen == 0 {
		padLen = aes.BlockSize
	}
	padded := make([]byte, len(pt)+padLen)
	copy(padded, pt)
	for i := len(pt); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}

	// Encrypt padded plaintext (CTR)
	ciphertext := make([]byte, len(padded))
	ctr.XORKeyStream(ciphertext, padded)

	// Calculate HMAC over the encrypted ciphertext
	h := hmac.New(sha256.New, hmacKey)
	h.Write(ciphertext)
	hmacDigest := h.Sum(nil)

	// Inner hex components
	saltHex := hex.EncodeToString(salt)
	hmacHex := hex.EncodeToString(hmacDigest)
	ciphertextHex := hex.EncodeToString(ciphertext)

	// Per spec, the vaulttext stored is hexlify( hexSalt + "\n" + hexHmac + "\n" + hexCipher )
	inner := saltHex + "\n" + hmacHex + "\n" + ciphertextHex
	outer := hex.EncodeToString([]byte(inner))

	return &AnsibleVaultString{
		FormatId:   "$ANSIBLE_VAULT",
		Version:    "1.1",
		CipherText: outer,
		VaultId:    "default",
	}, nil
}

func (a AnsibleVaultString) Decrypt(password string) (string, error) {
	// Outer hex decode
	outer, err := hex.DecodeString(a.CipherText)
	if err != nil {
		return "", fmt.Errorf("failed outer hex decode: %v", err)
	}
	// Split by newline bytes into 3 parts: hexSalt, hexHmac, hexCipher
	var hexSalt, hexHmac, hexCipher string
	{
		// manual split for minimal deps
		data := string(outer)
		parts := strings.Split(data, "\n")
		if len(parts) != 3 {
			return "", fmt.Errorf("invalid vault payload parts: %d", len(parts))
		}
		hexSalt, hexHmac, hexCipher = parts[0], parts[1], parts[2]
	}

	salt, err := hex.DecodeString(hexSalt)
	if err != nil {
		return "", fmt.Errorf("failed to decode salt: %v", err)
	}
	expectedHmac, err := hex.DecodeString(hexHmac)
	if err != nil {
		return "", fmt.Errorf("failed to decode hmac: %v", err)
	}
	ciphertext, err := hex.DecodeString(hexCipher)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %v", err)
	}

	// Derive keys: 32 cipher, 32 hmac, 16 iv
	derivedKey := pbkdf2.Key([]byte(password), salt, 10000, 80, sha256.New)
	cipherKey := derivedKey[:32]
	hmacKey := derivedKey[32:64]
	iv := derivedKey[64:80]

	// Verify HMAC over ciphertext
	h := hmac.New(sha256.New, hmacKey)
	h.Write(ciphertext)
	calc := h.Sum(nil)
	if !hmac.Equal(calc, expectedHmac) {
		return "", fmt.Errorf("HMAC verification failed")
	}

	// AES-CTR decrypt
	block, err := aes.NewCipher(cipherKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %v", err)
	}
	ctr := cipher.NewCTR(block, iv)
	plaintext := make([]byte, len(ciphertext))
	ctr.XORKeyStream(plaintext, ciphertext)
	// Strip RFC5652/PKCS7 padding
	if len(plaintext) == 0 {
		return "", fmt.Errorf("empty plaintext")
	}
	pad := int(plaintext[len(plaintext)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(plaintext) {
		return "", fmt.Errorf("invalid padding")
	}
	for i := len(plaintext) - pad; i < len(plaintext); i++ {
		if int(plaintext[i]) != pad {
			return "", fmt.Errorf("invalid padding")
		}
	}
	plaintext = plaintext[:len(plaintext)-pad]
	return string(plaintext), nil
}
