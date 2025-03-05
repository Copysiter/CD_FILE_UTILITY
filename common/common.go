package common

import (
	"crypto/tls"
	"fmt"
	"fts-cd-file-utility/cfg"
	"github.com/labstack/echo/v4"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var StartupConfig cfg.StartupConfig

var transCfg = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // ignore expired SSL certificates
}
var HttpClient = http.Client{
	Timeout: 0, // 30 * time.Second,
	Transport: transCfg,
}

func ReadConfig(c echo.Context) error {
	log.Println("readConfig")
	return c.JSONPretty(http.StatusOK, map[string]interface{}{
		"cfg": StartupConfig,
	}, "  ")
}

func CheckNfsStorageForReading(c echo.Context) error {
	log.Println("checkNfsStorageForReading")
	var fileList []string
	err := filepath.WalkDir(StartupConfig.NFSPath, func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			fileList = append(fileList, d.Name())
		}
		//log.Println(path, d.Name(), "directory?", d.IsDir())
		return nil
	})
	if err != nil {
		log.Println("failed to iterate through directory", StartupConfig.NFSPath, "because of", err)
		return err
	}
	return c.JSONPretty(http.StatusOK, map[string]interface{}{
		"fileList": fileList,
	}, "  ")
}
func CheckNfsStorageForWriting(c echo.Context) error {
	log.Println("checkNfsStorageForWriting")
	t := time.Now()
	filename := "tmp-" + t.Format("20060102150405") + ".txt"
	tmpFilePath := filepath.Join(StartupConfig.NFSPath, filename)
	f, err := os.Create(tmpFilePath)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create file in %s because of %s", tmpFilePath, err)
		log.Println(err)
		return c.JSONPretty(http.StatusConflict, map[string]interface{}{
			"success":      false,
			"errorMessage": errMsg,
		}, "  ")
	}
	if _, err = f.Write([]byte("My content\n")); err != nil {
		errMsg := fmt.Sprintf("failed to write to file %s because of %s", filepath.Join(StartupConfig.NFSPath, filename), err)
		log.Println(errMsg)
		return c.JSONPretty(http.StatusConflict, map[string]interface{}{
			"success":      false,
			"errorMessage": errMsg,
		}, "  ")
	}
	f.Close()
	time.Sleep(5 * time.Second)
	err = os.Remove(tmpFilePath)
	if err != nil {
		errMsg := fmt.Sprintf("failed to remove file %s because of %s", filepath.Join(StartupConfig.NFSPath, filename), err)
		log.Println(errMsg)
		return c.JSONPretty(http.StatusConflict, map[string]interface{}{
			"success":      false,
			"errorMessage": errMsg,
		}, "  ")
	}

	return c.JSONPretty(http.StatusConflict, map[string]interface{}{
		"success": true,
	}, "  ")
}

func GetJobMetaFileName(jobId string) string {
	return jobId + ".job"
}

func GetDownloadFileNameFromUrl(urlPath string) (string, error) {
	downloadUrl, err := url.Parse(urlPath)
	if err != nil {
		log.Println(urlPath, "is not a valid url")
		return "", err
	}
	pathParts := strings.Split(downloadUrl.Path, "/")
	fileName := pathParts[len(pathParts)-1]
	return fileName, nil
}
