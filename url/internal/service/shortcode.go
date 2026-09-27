package service

import "crypto/rand"

const (
	base62Alphabet  = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	shortCodeLength = 7
)

func generateShortCode() (string, error) {
	out := make([]byte, 0, shortCodeLength)
	buf := make([]byte, shortCodeLength*2)

	for len(out) < shortCodeLength {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if v := b & 63; v < 62 { // 0..63 uniform; drop 62 and 63
				out = append(out, base62Alphabet[v])
				if len(out) == shortCodeLength {
					break
				}
			}
		}
	}
	return string(out), nil
}
