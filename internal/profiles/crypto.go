package profiles

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

type CryptoProfile struct{}

func (p *CryptoProfile) ID() string { return "crypto" }

func (p *CryptoProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "hash",
			Description: "Hash text using MD5, SHA1, SHA256, or SHA512",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":      map[string]interface{}{"type": "string", "description": "Text to hash"},
					"algorithm": map[string]interface{}{"type": "string", "description": "Algorithm: md5, sha1, sha256, sha512 (default sha256)"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "hmac_sign",
			Description: "Create an HMAC signature for a message",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message":   map[string]interface{}{"type": "string", "description": "Message to sign"},
					"secret":    map[string]interface{}{"type": "string", "description": "Secret key"},
					"algorithm": map[string]interface{}{"type": "string", "description": "Algorithm: sha256, sha512 (default sha256)"},
				},
				"required": []string{"message", "secret"},
			},
		},
		{
			Name:        "generate_uuid",
			Description: "Generate a UUID v4 (random)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"count": map[string]interface{}{"type": "integer", "description": "Number of UUIDs to generate (default 1, max 50)"},
				},
			},
		},
		{
			Name:        "generate_password",
			Description: "Generate a secure random password",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"length": map[string]interface{}{"type": "integer", "description": "Password length (default 24, max 128)"},
					"charset": map[string]interface{}{
						"type":        "string",
						"description": "Character set: alphanumeric, ascii, hex, base64 (default ascii)",
					},
				},
			},
		},
		{
			Name:        "generate_random_bytes",
			Description: "Generate random bytes (hex or base64 encoded)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"length":   map[string]interface{}{"type": "integer", "description": "Number of bytes (default 32, max 256)"},
					"encoding": map[string]interface{}{"type": "string", "description": "Output encoding: hex, base64 (default hex)"},
				},
			},
		},
		{
			Name:        "jwt_decode",
			Description: "Decode a JWT token (without verifying signature)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"token": map[string]interface{}{"type": "string", "description": "JWT token to decode"},
				},
				"required": []string{"token"},
			},
		},
	}
}

func (p *CryptoProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "hash":
		return p.hash(args)
	case "hmac_sign":
		return p.hmacSign(args)
	case "generate_uuid":
		return p.generateUUID(args)
	case "generate_password":
		return p.generatePassword(args)
	case "generate_random_bytes":
		return p.generateRandomBytes(args)
	case "jwt_decode":
		return p.jwtDecode(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *CryptoProfile) hash(args map[string]interface{}) (string, error) {
	text := getStr(args, "text")
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	algo := getStr(args, "algorithm")
	if algo == "" {
		algo = "sha256"
	}
	data := []byte(text)
	switch strings.ToLower(algo) {
	case "md5":
		h := md5.Sum(data)
		return fmt.Sprintf("MD5: %s", hex.EncodeToString(h[:])), nil
	case "sha1":
		h := sha1.Sum(data)
		return fmt.Sprintf("SHA1: %s", hex.EncodeToString(h[:])), nil
	case "sha256":
		h := sha256.Sum256(data)
		return fmt.Sprintf("SHA256: %s", hex.EncodeToString(h[:])), nil
	case "sha512":
		h := sha512.Sum512(data)
		return fmt.Sprintf("SHA512: %s", hex.EncodeToString(h[:])), nil
	default:
		return "", fmt.Errorf("unsupported algorithm: %s (use md5, sha1, sha256, sha512)", algo)
	}
}

func (p *CryptoProfile) hmacSign(args map[string]interface{}) (string, error) {
	message := getStr(args, "message")
	secret := getStr(args, "secret")
	if message == "" || secret == "" {
		return "", fmt.Errorf("message and secret are required")
	}
	algo := getStr(args, "algorithm")
	if algo == "" {
		algo = "sha256"
	}
	key := []byte(secret)
	switch strings.ToLower(algo) {
	case "sha256":
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(message))
		return fmt.Sprintf("HMAC-SHA256: %s", hex.EncodeToString(mac.Sum(nil))), nil
	case "sha512":
		mac := hmac.New(sha512.New, key)
		mac.Write([]byte(message))
		return fmt.Sprintf("HMAC-SHA512: %s", hex.EncodeToString(mac.Sum(nil))), nil
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algo)
	}
}

func (p *CryptoProfile) generateUUID(args map[string]interface{}) (string, error) {
	count := int(getFloat(args, "count"))
	if count <= 0 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	var uuids []string
	for i := 0; i < count; i++ {
		b := make([]byte, 16)
		rand.Read(b)
		b[6] = (b[6] & 0x0f) | 0x40 // Version 4
		b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
		uuids = append(uuids, fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]))
	}
	if count == 1 {
		return uuids[0], nil
	}
	return strings.Join(uuids, "\n"), nil
}

func (p *CryptoProfile) generatePassword(args map[string]interface{}) (string, error) {
	length := int(getFloat(args, "length"))
	if length <= 0 {
		length = 24
	}
	if length > 128 {
		length = 128
	}
	charset := getStr(args, "charset")
	if charset == "" {
		charset = "ascii"
	}

	var chars string
	switch charset {
	case "alphanumeric":
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	case "ascii":
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?"
	case "hex":
		chars = "0123456789abcdef"
	case "base64":
		b := make([]byte, length)
		rand.Read(b)
		return base64.RawURLEncoding.EncodeToString(b)[:length], nil
	default:
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?"
	}

	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result), nil
}

func (p *CryptoProfile) generateRandomBytes(args map[string]interface{}) (string, error) {
	length := int(getFloat(args, "length"))
	if length <= 0 {
		length = 32
	}
	if length > 256 {
		length = 256
	}
	encoding := getStr(args, "encoding")
	if encoding == "" {
		encoding = "hex"
	}
	b := make([]byte, length)
	rand.Read(b)
	switch encoding {
	case "hex":
		return hex.EncodeToString(b), nil
	case "base64":
		return base64.StdEncoding.EncodeToString(b), nil
	default:
		return hex.EncodeToString(b), nil
	}
}

func (p *CryptoProfile) jwtDecode(args map[string]interface{}) (string, error) {
	token := getStr(args, "token")
	if token == "" {
		return "", fmt.Errorf("token is required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format (expected 3 parts, got %d)", len(parts))
	}

	decodeSegment := func(seg string) (string, error) {
		// Add padding if needed
		switch len(seg) % 4 {
		case 2:
			seg += "=="
		case 3:
			seg += "="
		}
		data, err := base64.URLEncoding.DecodeString(seg)
		if err != nil {
			return "", err
		}
		var pretty map[string]interface{}
		if err := json.Unmarshal(data, &pretty); err != nil {
			return string(data), nil
		}
		formatted, _ := json.MarshalIndent(pretty, "  ", "  ")
		return string(formatted), nil
	}

	header, err := decodeSegment(parts[0])
	if err != nil {
		return "", fmt.Errorf("failed to decode header: %s", err)
	}
	payload, err := decodeSegment(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode payload: %s", err)
	}

	return fmt.Sprintf("Header:\n  %s\n\nPayload:\n  %s\n\nSignature: %s\n\n⚠️ Signature NOT verified (decode only)", header, payload, parts[2]), nil
}
