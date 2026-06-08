package lyrics

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"strings"
	"testing"
)

// 测试内解密辅助：AES-128-ECB + PKCS7 去填充
func aesECBDecryptForTest(t *testing.T, src, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	bs := block.BlockSize()
	if len(src) == 0 || len(src)%bs != 0 {
		t.Fatalf("bad ciphertext length %d", len(src))
	}
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += bs {
		block.Decrypt(out[i:i+bs], src[i:i+bs])
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > bs {
		t.Fatalf("bad padding %d", pad)
	}
	return out[:len(out)-pad]
}

func TestEapiEncryptParamsRoundTrip(t *testing.T) {
	path := "/api/song/lyric/v1"
	text := `{"id":"123","yrc":"0"}`

	params := eapiEncryptParams(path, text)

	if params != strings.ToUpper(params) {
		t.Errorf("params 应为大写 hex，得到 %q", params)
	}
	raw, err := hex.DecodeString(params)
	if err != nil {
		t.Fatalf("params 非合法 hex: %v", err)
	}

	plain := aesECBDecryptForTest(t, raw, []byte(eapiKey))
	parts := bytes.Split(plain, []byte("-36cd479b6b5-"))
	if len(parts) != 3 {
		t.Fatalf("解密后应为 3 段，得到 %d 段: %q", len(parts), plain)
	}
	if string(parts[0]) != path {
		t.Errorf("path 段 = %q, want %q", parts[0], path)
	}
	if string(parts[1]) != text {
		t.Errorf("text 段 = %q, want %q", parts[1], text)
	}
	if len(parts[2]) != 32 {
		t.Errorf("digest 段应为 32 位 md5 hex，得到 %q", parts[2])
	}

	if eapiEncryptParams(path, text) != params {
		t.Error("eapiEncryptParams 应是确定性的")
	}
}
