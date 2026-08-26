package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const roomAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func randomHex(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func randomRoomCode() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}

	code := make([]byte, 8)
	for index, item := range value {
		code[index] = roomAlphabet[int(item)%len(roomAlphabet)]
	}
	return string(code), nil
}

func normalizeRoomCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeName(value string) (string, error) {
	name := norm.NFC.String(strings.TrimSpace(value))
	count := utf8.RuneCountInString(name)
	if count < 1 || count > 24 {
		return "", fmt.Errorf("name must contain between 1 and 24 characters")
	}

	for _, char := range name {
		if unicode.IsControl(char) {
			return "", fmt.Errorf("name contains a control character")
		}
	}

	return name, nil
}
