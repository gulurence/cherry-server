package worldsim

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
)

// 网格串行程编码（RLE）：把 1,000,000 字符的地形/城堡快照（每格一个字节）压成紧凑字节流，
// 再 base64 成 JSON 字符串字段可安全传输的文本。原始字节格式（base64 解码后）：
//
//	uvarint(N)                        // 总格子数 N（= MapW*MapH）
//	{ value byte, uvarint(runLen) }*  // 连续相同格子的 (值, 长度) 对，依次覆盖 N 格
//
// value 为原始字符 '0'/'1'/'2'（地形）或 '0'/'1'（城堡）。山/河是成片块、城堡是 8×8 实心块，
// 行程很长，压缩率通常 10× 以上。
//
// 与客户端解码（stress.go 的 decodeGrid、MFC GridWorldClient 的 DecodeGridString）一一对应，
// 改格式需三端同步。
func appendUvarint(dst []byte, v uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	return append(dst, buf[:n]...)
}

// EncodeGridRLE 把网格串 s 行程编码成字节流。
func EncodeGridRLE(s string) []byte {
	out := make([]byte, 0, 4096)
	out = appendUvarint(out, uint64(len(s)))
	if len(s) == 0 {
		return out
	}
	prev := s[0]
	run := 1
	for i := 1; i < len(s); i++ {
		if s[i] == prev {
			run++
		} else {
			out = append(out, prev)
			out = appendUvarint(out, uint64(run))
			prev = s[i]
			run = 1
		}
	}
	out = append(out, prev)
	out = appendUvarint(out, uint64(run))
	return out
}

// DecodeGridRLE 把 EncodeGridRLE 的输出还原成等长网格串。expect 为期望格子数，长度不一致
// 或字节流非法时返回 error。
func DecodeGridRLE(b []byte, expect int) (string, error) {
	n, pos := binary.Uvarint(b)
	if pos <= 0 {
		return "", errors.New("grid rle: bad total length")
	}
	if int(n) != expect {
		return "", fmt.Errorf("grid rle: total %d != expect %d", n, expect)
	}
	out := make([]byte, 0, expect)
	for len(out) < expect {
		if pos >= len(b) {
			return "", errors.New("grid rle: truncated value")
		}
		val := b[pos]
		pos++
		run, k := binary.Uvarint(b[pos:])
		if k <= 0 {
			return "", errors.New("grid rle: bad run length")
		}
		pos += k
		if run == 0 || int(run) > expect-len(out) {
			return "", errors.New("grid rle: run overflow")
		}
		for i := uint64(0); i < run; i++ {
			out = append(out, val)
		}
	}
	return string(out), nil
}

// EncodeGridBase64 行程编码 + base64，用于 EnterResponse 的 map/castles 字符串字段。
func EncodeGridBase64(s string) string {
	return base64.StdEncoding.EncodeToString(EncodeGridRLE(s))
}

// DecodeGridBase64 是 EncodeGridBase64 的逆操作。
func DecodeGridBase64(s string, expect int) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return DecodeGridRLE(b, expect)
}
