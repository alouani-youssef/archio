package objects

import (
	"strings"
	"testing"
)

func TestValidateKey(t *testing.T) {
	valid := []string{"a", "photos/été 2026/a b.jpg", strings.Repeat("a", 1024)}
	invalid := []string{"", strings.Repeat("a", 1025), "\x00", "\xff", "/a", "a/", "a//b", "a/./b", "a/../b"}
	for _, k := range valid {
		if err := ValidateKey(k); err != nil {
			t.Errorf("%q should be valid: %v", k, err)
		}
	}
	for _, k := range invalid {
		if err := ValidateKey(k); err == nil {
			t.Errorf("%q should be invalid", k)
		}
	}
}
