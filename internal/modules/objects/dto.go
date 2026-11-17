package objects

import "time"

type versionResponse struct {
	VersionID      string    `json:"versionId"`
	Key            string    `json:"key"`
	Size           int64     `json:"size"`
	ChecksumSha256 string    `json:"checksumSha256"`
	ContentType    string    `json:"contentType"`
	CreatedAt      time.Time `json:"createdAt"`
	ReadURL        string    `json:"readUrl"`
}

type objectResponse struct {
	Key            string           `json:"key"`
	UpdatedAt      time.Time        `json:"updatedAt"`
	CurrentVersion *versionResponse `json:"currentVersion"`
}

type listResponse struct {
	Objects    []objectResponse `json:"objects"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type versionsResponse struct {
	Versions []versionResponse `json:"versions"`
}
