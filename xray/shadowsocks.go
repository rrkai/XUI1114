package xray

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"

	"github.com/alireza0/x-ui/util/common"
)

var shadowsocks2022KeyLengths = map[string]int{
	"2022-blake3-aes-128-gcm":       16,
	"2022-blake3-aes-256-gcm":       32,
	"2022-blake3-chacha20-poly1305": 32,
}

// Shadowsocks2022KeyLength is the key size a Shadowsocks 2022 method needs,
// or 0 for any other method, which takes a free-form password.
func Shadowsocks2022KeyLength(method string) int {
	return shadowsocks2022KeyLengths[method]
}

// ValidShadowsocks2022Key matches the core: the key is standard base64 and
// decodes to exactly length bytes.
func ValidShadowsocks2022Key(key string, length int) bool {
	if length <= 0 {
		return true
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(decoded) == length
}

func NewShadowsocks2022Key(length int) string {
	key := make([]byte, length)
	if _, err := rand.Read(key); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(key)
}

// CheckShadowsocks2022Keys rejects inbound settings holding a Shadowsocks 2022
// key of the wrong size. The core refuses such a key while building the
// inbound, which keeps all of Xray from starting, not only this inbound.
func CheckShadowsocks2022Keys(protocol string, settings string) error {
	if protocol != "shadowsocks" {
		return nil
	}
	var parsed struct {
		Method   string `json:"method"`
		Password string `json:"password"`
		Clients  []struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		} `json:"clients"`
	}
	if json.Unmarshal([]byte(settings), &parsed) != nil {
		return nil
	}
	length := Shadowsocks2022KeyLength(parsed.Method)
	if length == 0 {
		return nil
	}
	if !ValidShadowsocks2022Key(parsed.Password, length) {
		return common.NewErrorf("%s needs a base64 key of %d bytes, the inbound password is not one", parsed.Method, length)
	}
	for _, client := range parsed.Clients {
		if !ValidShadowsocks2022Key(client.Password, length) {
			return common.NewErrorf("%s needs a base64 key of %d bytes, the password of %s is not one", parsed.Method, length, client.Email)
		}
	}
	return nil
}
