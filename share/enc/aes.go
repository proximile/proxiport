package enc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// DeriveKeyArgon2id derives a 32-byte AES-256 key from a passphrase using
// Argon2id with a caller-supplied per-vault salt and cost parameters. This is
// the key-derivation function for the vault; the salt and parameters are stored
// alongside the ciphertext so the same key can be re-derived on unlock.
func DeriveKeyArgon2id(password string, salt []byte, timeCost, memoryKiB uint32, threads uint8) []byte {
	return argon2.IDKey([]byte(password), salt, timeCost, memoryKiB, threads, 32)
}

// DeriveKeyLegacySHA256 reproduces the original (weak, unsalted) key derivation.
// It exists only so a vault written by the old code can be read once during the
// transparent re-key to Argon2id; never use it to write new data.
func DeriveKeyLegacySHA256(password string) []byte {
	sum := sha256.Sum256([]byte(password))
	return sum[:]
}

// Aes256EncryptByKeyToBase64String encrypts payload with a 32-byte AES-256 key
// (AES-GCM) and returns base64-encoded ciphertext.
func Aes256EncryptByKeyToBase64String(payload, key []byte) (string, error) {
	encryptedBytes, err := Aes256Encrypt(payload, key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encryptedBytes), nil
}

// Aes256DecryptByKeyFromBase64String is the reverse of
// Aes256EncryptByKeyToBase64String: it base64-decodes and AES-GCM-decrypts with
// the given 32-byte key. A wrong key fails the GCM authentication tag and
// returns an error.
func Aes256DecryptByKeyFromBase64String(encryptedBase64Data string, key []byte) ([]byte, error) {
	encryptedBytesData, err := base64.StdEncoding.DecodeString(encryptedBase64Data)
	if err != nil {
		return nil, err
	}
	return AesDecrypt(encryptedBytesData, key)
}

func Aes256Encrypt(payload, aes32Key []byte) (encryptedData []byte, err error) {
	keyLen := len(aes32Key)
	if keyLen != 32 {
		err = fmt.Errorf("invalid aes32Key length: a 32 bytes key is expected but %d byts key is provided", keyLen)
		return
	}

	//Create a new Cipher Block from the aes32Key
	block, err := aes.NewCipher(aes32Key)
	if err != nil {
		return encryptedData, err
	}

	//Create a new GCM - https://en.wikipedia.org/wiki/Galois/Counter_Mode
	//https://golang.org/pkg/crypto/cipher/#NewGCM
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return encryptedData, err
	}

	//Create a nonce. Nonce should be from GCM
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return encryptedData, err
	}

	//Encrypt the data using aesGCM.Seal
	//Since we don't want to save the nonce somewhere else in this case, we add it as a prefix to the encrypted data. The first nonce argument in Seal is the prefix.
	ciphertext := aesGCM.Seal(nonce, nonce, payload, nil)
	return ciphertext, nil
}

func AesDecrypt(encryptedData, key []byte) (decryptedData []byte, err error) {
	//Create a new Cipher Block from the key
	block, err := aes.NewCipher(key)
	if err != nil {
		return decryptedData, err
	}

	//Create a new GCM
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return decryptedData, err
	}

	//Get the nonce size
	nonceSize := aesGCM.NonceSize()

	if len(encryptedData) <= nonceSize {
		return decryptedData, fmt.Errorf("invalid encrypted value provided: invalid nonce length")
	}

	//Extract the nonce from the encrypted data
	nonce, ciphertext := encryptedData[:nonceSize], encryptedData[nonceSize:]

	//Decrypt the data
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return decryptedData, err
	}

	return plaintext, nil
}
