package encoding

import (
	"encoding/base64"
	"errors"
)

const (
	CUSTOM_ALPHABET = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
	STD_ALPHABET   = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	CUSTOM_PAD     = '$'
)

var c2s [128]int
var s2c [128]int

func init() {
	for i := 0; i < 128; i++ {
		c2s[i] = -1
		s2c[i] = -1
	}
	for i := 0; i < 64; i++ {
		c2s[CUSTOM_ALPHABET[i]] = int(STD_ALPHABET[i])
		s2c[STD_ALPHABET[i]] = int(CUSTOM_ALPHABET[i])
	}
	c2s[CUSTOM_PAD] = '='
	s2c['='] = int(CUSTOM_PAD)
}

func Encode(plaintext []byte) string {
	std := base64.StdEncoding.EncodeToString(plaintext)
	n := len(std)
	a := n / 3
	rearranged := std[n-a:] + std[a:n-a] + std[:a]
	
	result := make([]byte, n)
	for i := 0; i < n; i++ {
		c := int(rearranged[i])
		var m int
		if c < 128 {
			m = s2c[c]
		} else {
			m = -1
		}
		if m < 0 {
			panic(errors.New("char out of alphabet"))
		}
		result[i] = byte(m)
	}
	return string(result)
}

func Decode(encoded string) ([]byte, error) {
	n := len(encoded)
	mapped := make([]byte, n)
	for i := 0; i < n; i++ {
		c := int(encoded[i])
		var m int
		if c < 128 {
			m = c2s[c]
		} else {
			m = -1
		}
		if m < 0 {
			return nil, errors.New("char out of custom alphabet")
		}
		mapped[i] = byte(m)
	}
	a := n / 3
	std := string(mapped[n-a:]) + string(mapped[a:n-a]) + string(mapped[:a])
	return base64.StdEncoding.DecodeString(std)
}
