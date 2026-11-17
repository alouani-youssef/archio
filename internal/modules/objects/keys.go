package objects

import (
	"net/http"
	"strings"
	"unicode/utf8"

	apperr "archio/pkg/errors"
)

const maxKeyBytes = 1024

var errInvalidKey = apperr.New(http.StatusBadRequest, "invalid_object_key",
	"object key must be 1-1024 bytes of valid UTF-8 without NUL, leading or trailing '/', empty, '.' or '..' segments")

// ValidateKey checks an object key against the instance-wide key rules.
func ValidateKey(key string) error {
	if key == "" || len(key) > maxKeyBytes || !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
		return errInvalidKey
	}
	// An empty segment covers a leading '/', a trailing '/' and '//'.
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errInvalidKey
		}
	}
	return nil
}
