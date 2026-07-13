package enc

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAesEncryptDecrypt(t *testing.T) {
	testCases := []struct {
		name    string
		pass    string
		payload string
	}{
		{
			name:    "one_char_pass",
			pass:    "1",
			payload: "hello aes",
		},
		{
			name:    "two_char_pass",
			pass:    "12",
			payload: "hello aes",
		},
		{
			name:    "eight_char_pass",
			pass:    "12345678",
			payload: "hello aes",
		},
		{
			name:    "utf8_chars",
			pass:    "ж国语",
			payload: "國語",
		},
		{
			name:    "very_long_pass",
			pass:    "12345678901234567890123456789012",
			payload: "alala",
		},
	}

	salt := []byte("0123456789abcdef")
	for i := range testCases {
		t.Run(testCases[i].name, func(t *testing.T) {
			key := DeriveKeyArgon2id(testCases[i].pass, salt, 1, 8*1024, 1)
			require.Len(t, key, 32)

			encData, err := Aes256EncryptByKeyToBase64String([]byte(testCases[i].payload), key)
			require.NoError(t, err)
			assert.NotEqual(t, testCases[i].payload, encData)

			decData, err := Aes256DecryptByKeyFromBase64String(encData, key)
			require.NoError(t, err)
			assert.Equal(t, testCases[i].payload, string(decData))

			// A different passphrase derives a different key and fails the GCM tag.
			wrongKey := DeriveKeyArgon2id(testCases[i].pass+"x", salt, 1, 8*1024, 1)
			_, err = Aes256DecryptByKeyFromBase64String(encData, wrongKey)
			assert.Error(t, err)
		})
	}
}

func TestAesEncryptWrongKeySize(t *testing.T) {
	keysWithWrongSize := []string{"string_with_more_than_32_characters"}
	for charsCount := 1; charsCount < 32; charsCount++ {
		keyWithWrongSize := ""
		for i := 0; i < charsCount; i++ {
			keyWithWrongSize += "a"
		}
		keysWithWrongSize = append(keysWithWrongSize, keyWithWrongSize)
	}

	for i := range keysWithWrongSize {
		_, err := Aes256Encrypt([]byte("some payload"), []byte(keysWithWrongSize[i]))
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"invalid aes32Key length: a 32 bytes key is expected but %d byts key is provided",
				len(keysWithWrongSize[i]),
			),
			len(keysWithWrongSize[i]),
		)
	}
}
