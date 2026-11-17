package auth

import (
	"regexp"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	re := regexp.MustCompile(`^ak_[A-Za-z0-9_-]{43}$`)
	p1, prefix, hash, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString(p1) {
		t.Fatalf("bad plaintext format")
	}
	if prefix != p1[:11] {
		t.Fatalf("prefix = %q", prefix)
	}
	if hash != HashKey(p1) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
		t.Fatalf("bad hash %q", hash)
	}
	p2, _, _, _ := GenerateKey()
	if p1 == p2 {
		t.Fatal("keys should differ")
	}
}
