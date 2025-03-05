package common

type NexusSearchResponse struct {
	Items []NexusItem `json:"items,omitempty"`
}

type NexusItemAsset struct {
	DownloadUrl string `json:"downloadUrl,omitempty"`
	FileSize    int64  `json:"fileSize,omitempty"`
}

type NexusItem struct {
	Id         string           `json:"id,omitempty"`
	Repository string           `json:"repository,omitempty"`
	Format     string           `json:"format,omitempty"`
	Name       string           `json:"name,omitempty"`
	Version    string           `json:"version,omitempty"`
	Assets     []NexusItemAsset `json:"assets,omitempty"`
}
