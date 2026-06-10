package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "s3cret" || hash == "" {
		t.Fatalf("哈希不应等于明文/为空: %q", hash)
	}
	if !CheckPassword(hash, "s3cret") {
		t.Error("正确密码应通过")
	}
	if CheckPassword(hash, "wrong") {
		t.Error("错误密码应拒绝")
	}
}
