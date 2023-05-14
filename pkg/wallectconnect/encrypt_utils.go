/*
 *  Copyright © 成都黢黑数字科技有限公司 - All Rights Reserved
 *  * Unauthorized copying of this file, via any medium is strictly prohibited
 *  * Proprietary and confidential
 *  * Written by 李相君 lxjpub@gmail.com, April 2019
 *
 */

package wallectconnect

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"moff.io/moff-social/pkg/errors"
	"strings"
)

func Aes256Encrypt(content, encryptionKey, iv []byte) ([]byte, error) {
	bPlaintext := pkcs5Padding(content, aes.BlockSize)
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, errors.Wrap(err, "create new cipher block")
	}
	ciphertext := make([]byte, len(bPlaintext))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, bPlaintext)
	return ciphertext, nil
}

func Aes256Decrypt(cipherText []byte, encryptionKey []byte, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, errors.Wrap(err, "create new cipher block")
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(cipherText, cipherText)

	cutTrailingSpaces := []byte(strings.TrimSpace(string(cipherText)))
	return cutTrailingSpaces, nil
}

func pkcs5Padding(cipherText []byte, blockSize int) []byte {
	padding := blockSize - len(cipherText)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(cipherText, padText...)
}

func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func HmacSha256(data, secret []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write(data)
	return h.Sum(nil)
}
