package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type TokenClaims struct {
	UserID string `json:"userId"`
	Expiry int64  `json:"exp"`
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func SignToken(userID, secret string) (string, error) {
	if userID == "" || len(secret) < 32 {
		return "", errors.New("JWT signing is not configured")
	}
	header, _ := json.Marshal(jwtHeader{Algorithm: "HS256", Type: "JWT"})
	claims, _ := json.Marshal(TokenClaims{UserID: userID, Expiry: time.Now().Add(24 * time.Hour).Unix()})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	signature := sign(unsigned, secret)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func VerifyToken(token, secret string, now time.Time) (TokenClaims, error) {
	var claims TokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(secret) < 32 {
		return claims, errors.New("invalid token")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, errors.New("invalid token")
	}
	var header jwtHeader
	if err := decodeStrict(headerBytes, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return claims, errors.New("invalid token")
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(providedSignature, sign(parts[0]+"."+parts[1], secret)) {
		return claims, errors.New("invalid token")
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || decodeStrict(claimsBytes, &claims) != nil || claims.UserID == "" || claims.Expiry <= now.Unix() {
		return TokenClaims{}, errors.New("invalid token")
	}
	return claims, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func sign(message, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return mac.Sum(nil)
}
