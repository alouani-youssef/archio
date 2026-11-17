package middlewares

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	apperr "archio/pkg/errors"

	"archio/internal/auth"
	"github.com/google/uuid"
)

// KeyAuthenticator resolves an API key hash to its group. A returned
// *apperr.Error with status 401 means the key was rejected.
type KeyAuthenticator interface {
	Authenticate(ctx context.Context, keyHash string) (uuid.UUID, error)
}

func equalDigest(a, b string) bool {
	da, db := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(da[:], db[:]) == 1
}

// BasicAuth guards a handler with HTTP Basic credentials.
func BasicAuth(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			userOK := equalDigest(u, username)
			passOK := equalDigest(p, password)
			if !ok || !userOK || !passOK {
				w.Header().Set("WWW-Authenticate", `Basic realm="archio"`)
				apperr.Write(w, apperr.New(http.StatusUnauthorized, "unauthorized", "invalid credentials"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// APIKeyAuth authenticates Bearer API keys and stores the group in the context.
func APIKeyAuth(a KeyAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
			token = strings.TrimSpace(token)
			if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
				apperr.Write(w, apperr.New(http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token"))
				return
			}
			groupID, err := a.Authenticate(r.Context(), auth.HashKey(token))
			if err != nil {
				var appErr *apperr.Error
				if errors.As(err, &appErr) {
					apperr.Write(w, appErr)
				} else {
					apperr.Write(w, err)
				}
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithGroup(r.Context(), groupID)))
		})
	}
}
