// Package token 提供登录服的会话令牌签发与校验。
//
// 令牌是一段 base64(JSON{uid, timestamp, sign})，sign = md5(uid + timestamp + 密钥)。
// 密钥写在各节点的 __settings__.token_secret 里，登录服与网关保持一致即可。
package token

import (
	"encoding/json"
	"fmt"

	ccrypto "github.com/gulurence/cherry-server/extend/crypto"
	ctime "github.com/gulurence/cherry-server/extend/time"
)

const (
	signFormat = "uid:%d,timestamp:%d"
)

// Token 是登录服签发的会话令牌。
type Token struct {
	UID       int64  `json:"uid"`
	Timestamp int64  `json:"timestamp"`
	Sign      string `json:"sign"`
}

// New 使用密钥为 uid 签发一个令牌。
func New(uid int64, secret string) *Token {
	t := &Token{
		UID:       uid,
		Timestamp: ctime.Now().ToMillisecond(),
	}
	t.Sign = sign(t, secret)
	return t
}

// Verify 校验令牌并返回其中的 uid。令牌非法返回 (0, false)。
func Verify(tokenStr, secret string) (int64, bool) {
	if tokenStr == "" {
		return 0, false
	}

	t := &Token{}
	data, err := ccrypto.Base64DecodeBytes(tokenStr)
	if err != nil {
		return 0, false
	}
	if err = json.Unmarshal(data, t); err != nil {
		return 0, false
	}
	if t.UID < 1 || t.Sign != sign(t, secret) {
		return 0, false
	}
	return t.UID, true
}

// String 返回 base64 编码的令牌字符串。
func (t *Token) String() string {
	data, _ := json.Marshal(t)
	return ccrypto.Base64Encode(string(data))
}

func sign(t *Token, secret string) string {
	return ccrypto.MD5(fmt.Sprintf(signFormat, t.UID, t.Timestamp) + secret)
}
