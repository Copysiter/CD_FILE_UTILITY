package common

import (
	"io"
	"time"
)

type Artifact interface {
	GetOriginalResourceName() string

	GetArtifactNameAndStream() (ArtifactNameAndStream, error)

	/*
	 * performs cleanup on send-server
	 * E.g. removes downloaded docker image
	 */
	DeliverCleanup() error

	/*
	 * performs cleanup on receive-server
	 * E.g. removes downloaded docker image
	 */
	DeployCleanup() error

	GetType() ArtifactType
}

type ArtifactNameAndStream struct {
	Name   string
	Stream io.ReadCloser
}

type Job struct {
	Artifact string `json:"artifact"`
}

type PypiJob struct {
	Artifact string `json:"package"`
	Version  string `json:"version"`
}

type HfJob struct {
	Artifact string `json:"model"`
}

type JobStatus struct {
	Artifact     Artifact     `json:"artifact"`
	ArtifactType ArtifactType `json:"artifactType"`
	ArtifactPath string       `json:"path"`
	Status       CdStatus     `json:"status"`
	StatusDttm   time.Time    `json:"statusDttm"`
	// Данные о фрагментации файла
	IsChunked    bool         `json:"isChunked,omitempty"`
	ChunkCount   int          `json:"chunkCount,omitempty"`
	TotalSize    int64        `json:"totalSize,omitempty"`
	Chunks       []FileChunk  `json:"chunks,omitempty"`
	// Хеш-суммы файла
	MD5Hash      string       `json:"md5Hash,omitempty"`
	SHA256Hash   string       `json:"sha256Hash,omitempty"`
	Hash         string       `json:"hash,omitempty"`
}

type CdStatus string
type ArtifactType string

const (
	DOWNLOADING           CdStatus     = "DOWNLOADING"
	DOWNLOADING_FAILED    CdStatus     = "DOWNLOADING_FAILED"
	META_WRITING_FAILED   CdStatus     = "META_WRITING_FAILED"
	DOWNLOADING_DONE      CdStatus     = "DOWNLOADING_DONE"
	CHUNKED              CdStatus     = "CHUNKED"
	CHUNK_DOWNLOADING    CdStatus     = "CHUNK_DOWNLOADING"
	CHUNK_DONE           CdStatus     = "CHUNK_DONE"
	CHUNKS_MERGING       CdStatus     = "CHUNKS_MERGING"
	CHUNKS_MERGE_FAILED  CdStatus     = "CHUNKS_MERGE_FAILED"
	SUCCESS               CdStatus     = "SUCCESS"
	DOCKER                ArtifactType = "DOCKER"
	PYPI                  ArtifactType = "PYPI"
	HF                    ArtifactType = "HF"
)
