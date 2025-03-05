package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"fts-cd-file-utility/cfg"
	_ "github.com/docker/docker/api/types/container"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

// var DockerApiVersion string

type PypiArtifact struct {
	PackageName string
	Version     string
}

func (a PypiArtifact) GetType() ArtifactType {
	return PYPI
}
func (a PypiArtifact) GetOriginalResourceName() string {
	return a.PackageName
}

func buildNexusSearchPackageVersionUrl(cfg *cfg.StartupConfig, artifact *PypiArtifact) string {
	return fmt.Sprintf("%s/service/rest/v1/search?sort=version&repository=%s&name=%s&version=%s", cfg.SendNexusUrl, cfg.SendNexusPypiRepository, artifact.PackageName, artifact.Version)
}

func (a PypiArtifact) GetArtifactNameAndStream() (ArtifactNameAndStream, error) {
	// search for artifact with specified version
	// if found - download it
	// if not found - search for artifact without version
	// show appropriate message
	// curl -u raisa:Qwerty123 'http://10.7.86.10:8081/service/rest/v1/search?repository=pypi-hosted&name=hello-world-package&version=0.1.3'
	searchUrl := buildNexusSearchPackageVersionUrl(&StartupConfig, &a)
	log.Println("searching for package with searchUrl ", searchUrl)
	req, err := http.NewRequest("GET", searchUrl, nil)
	req.SetBasicAuth(StartupConfig.SendNexusLogin, StartupConfig.SendNexusPassword)
	if err != nil {
		log.Printf("failed to create request; err: %v\n", err)
		return ArtifactNameAndStream{}, err
	}

	resp, err := HttpClient.Do(req)
	if err != nil {
		log.Printf("failed to make search request package %s:%s; err: %v\n", a.PackageName, a.Version, err)
		return ArtifactNameAndStream{}, err
	}

	parsedSearchResponse := new(NexusSearchResponse)
	err = json.NewDecoder(resp.Body).Decode(&parsedSearchResponse)
	if err != nil {
		log.Println("failed to decode nexus search response", err)
		return ArtifactNameAndStream{}, err
	}
	if len(parsedSearchResponse.Items) == 0 {
		msg := fmt.Sprintf("package %s:%s not found in Nexus", a.PackageName, a.Version)
		log.Println(msg)
		return ArtifactNameAndStream{}, errors.New(msg)
	}

	packageItem := parsedSearchResponse.Items[0]
	if len(packageItem.Assets) == 0 {
		msg := fmt.Sprintf("no asset found for package %s:%s", a.PackageName, a.Version)
		log.Println(msg)
		return ArtifactNameAndStream{}, errors.New(msg)
	}
	downloadUrl := packageItem.Assets[0].DownloadUrl
	log.Println("downloadUrl =", downloadUrl)
	downloadFileName, err := GetDownloadFileNameFromUrl(downloadUrl)
	if err != nil {
		return ArtifactNameAndStream{}, err
	}

	downloadReq, err := http.NewRequest("GET", downloadUrl, nil)
	if err != nil {
		log.Println("failed to create request", err)
		return ArtifactNameAndStream{}, err
	}
	downloadResp, err := HttpClient.Do(downloadReq)
	if err != nil {
		log.Println("failed to download remote file", err)
		return ArtifactNameAndStream{}, err
	}
	return ArtifactNameAndStream{Name: downloadFileName, Stream: downloadResp.Body}, nil
}

func (a PypiArtifact) DeliverCleanup() error {
	return nil
}
func (a PypiArtifact) DeployCleanup() error {
	return nil
}

func IsTwineInstalled() bool {
	/*
		Twine response to be parsed:

		/root/.local/lib/python3/site-packages/requests/__init__.py:102: RequestsDependencyWarning: urllib3 (1.26.19) or chardet (5.2.0)/charset_normalizer (2.0.12) doesn't match a supported version!
		  warnings.warn("urllib3 ({}) or chardet ({})/charset_normalizer ({}) doesn't match a supported "
		twine version 5.1.1 (importlib-metadata: 7.1.0, keyring: 25.3.0, pkginfo:
		1.10.0, requests: 2.26.0, requests-toolbelt: 1.0.0, urllib3: 1.26.19)
	*/
	cmd := exec.Command("twine", "--version")

	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	cmdOutput := out.String()

	if err != nil {
		log.Printf("Err: %v", err)
	}

	lines := strings.Split(cmdOutput, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "twine version") {
			log.Println(line)
			return true
		}
	}
	log.Println("----------- `twine --version` OUTPUT START -----------")
	log.Println("\n", cmdOutput)
	log.Println("----------- `twine --version` OUTPUT END   -----------")

	log.Fatalln("Failed to get Twine Version")
	return false
}
