package buckets

import "time"

type createRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type bucketResponse struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

type listResponse struct {
	Buckets []bucketResponse `json:"buckets"`
}
