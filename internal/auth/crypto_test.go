package auth

import (
	"crypto/rand"
	"io"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	ct, err := Encrypt(key, "subsonic-pw")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(ct) == "subsonic-pw" {
		t.Fatal("密文不应等于明文")
	}
	got, err := Decrypt(key, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "subsonic-pw" {
		t.Errorf("往返不一致: %q", got)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	ct, _ := Encrypt(testKey(t), "x")
	if _, err := Decrypt(testKey(t), ct); err == nil {
		t.Error("不同密钥解密应失败")
	}
}
