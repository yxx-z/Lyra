package lyrics

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"encoding/hex"
	"strings"
)

// eapiKey 是网易云 eapi 接口的 AES-128-ECB 密钥（公开常量）。
const eapiKey = "e82ckenh8dichen8"

// eapiEncryptParams 按网易云 eapi 协议加密请求体，返回大写 hex 字符串。
// path 为 API 路径（如 "/api/song/lyric/v1"），text 为 JSON 参数体。
func eapiEncryptParams(path, text string) string {
	digestInput := "nobody" + path + "use" + text + "md5forencrypt"
	sum := md5.Sum([]byte(digestInput))
	digest := hex.EncodeToString(sum[:])
	data := path + "-36cd479b6b5-" + text + "-36cd479b6b5-" + digest
	enc := aesECBEncrypt([]byte(data), []byte(eapiKey))
	return strings.ToUpper(hex.EncodeToString(enc))
}

// aesECBEncrypt 对 src 做 AES-ECB + PKCS7 填充加密。
func aesECBEncrypt(src, key []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	bs := block.BlockSize()
	pad := bs - len(src)%bs
	src = append(src, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += bs {
		block.Encrypt(out[i:i+bs], src[i:i+bs])
	}
	return out
}
