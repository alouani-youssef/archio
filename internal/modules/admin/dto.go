package admin

import "time"

type createRequest struct {
	Name string `json:"name"`
}

type groupResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type keyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

// createdKeyResponse is the only shape that ever carries the plaintext key.
type createdKeyResponse struct {
	keyResponse
	Key string `json:"key"`
}
