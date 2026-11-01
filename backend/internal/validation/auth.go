package validation

import (
	"net/mail"
	"strings"
)

func NormalizeEmail(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(email) > 254 {
		return "", false
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", false
	}
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || !strings.Contains(email[at+1:], ".") {
		return "", false
	}
	return email, true
}

func ValidPassword(password string) bool {
	return len(password) >= 8 && len(password) <= 72
}
