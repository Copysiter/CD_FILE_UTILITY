package common

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

type HttpArtifact struct {
	DownloadFilePath string
}

func (a HttpArtifact) GetOriginalResourceName() string {
	return a.DownloadFilePath
}
func (a HttpArtifact) GetDownloadFileName(jobId string) string {
	downloadUrl, err := url.Parse(a.DownloadFilePath)
	if err != nil {
		log.Println(a.DownloadFilePath, "is not a valid url")
		return jobId + ".file"
	}
	pathParts := strings.Split(downloadUrl.Path, "/")
	fileName := pathParts[len(pathParts)-1]
	return fileName
}

func (a HttpArtifact) GetStream() (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", a.DownloadFilePath, nil)
	if err != nil {
		log.Println("failed to create request", err)
		return nil, err
	}
	resp, err := HttpClient.Do(req)
	if err != nil {
		log.Println("failed to download remote file", err)
		return nil, err
	}
	return resp.Body, nil
}

func (a HttpArtifact) DeliverCleanup() error {
	return nil
}
