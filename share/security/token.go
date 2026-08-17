package security

import "crypto/rand"

const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func NewRandomToken(length int) (string, error) {
	// Largest multiple of len(chars) that fits in a byte. Bytes at or above this
	// threshold are discarded (rejection sampling) so the modulo mapping stays
	// uniform instead of skewing toward the first characters of the alphabet.
	const maxByte = 256 - (256 % len(chars))

	out := make([]byte, 0, length)
	buf := make([]byte, length)
	for len(out) < length {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if len(out) == length {
				break
			}
			if int(b) < maxByte {
				out = append(out, chars[int(b)%len(chars)])
			}
		}
	}

	return string(out), nil
}
