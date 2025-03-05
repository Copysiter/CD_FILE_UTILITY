package common

import (
	"context"
	"encoding/base64"
	"encoding/json"
	_ "github.com/docker/docker/api/types/container"
	image "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var DockerApiVersion string

type DockerArtifact struct {
	ImageName string
}

func (a DockerArtifact) GetOriginalResourceName() string {
	return a.ImageName
}
func (a DockerArtifact) GetDownloadFileName() string {
	imageName := a.ImageName
	imageName = strings.ReplaceAll(imageName, "/", "--")
	imageName = strings.ReplaceAll(imageName, ":", "-v")
	return imageName + ".image"
}

func buildPullOptions() (image.PullOptions, error) {
	if StartupConfig.SendDockerRegistryLogin != "" && StartupConfig.SendDockerRegistryPassword != "" {
		log.Println("making custom pull options with docker registry", StartupConfig.SendDockerRegistry)

		authConfig := registry.AuthConfig{
			Username:      StartupConfig.SendDockerRegistryLogin,
			Password:      StartupConfig.SendDockerRegistryPassword,
			ServerAddress: StartupConfig.SendDockerRegistry,
		}
		authConfigBytes, err := json.Marshal(authConfig)
		if err != nil {
			log.Printf("failed to marshal auth config for pull options. error: %v\n", err)
			return image.PullOptions{}, err
		}
		authConfigEncoded := base64.URLEncoding.EncodeToString(authConfigBytes)

		return image.PullOptions{RegistryAuth: authConfigEncoded}, nil
	}

	return image.PullOptions{}, nil
}

func (a DockerArtifact) GetStream() (io.ReadCloser, error) {
	imageName := a.ImageName
	apiClient, err := client.NewClientWithOpts(client.WithVersion(DockerApiVersion))
	if err != nil {
		log.Println("failed to create docker client", err)
		return nil, err
	}
	defer apiClient.Close()
	pullOptions, err := buildPullOptions()
	if err != nil {
		log.Println("failed to build pull options", err)
		return nil, err
	}
	tgtImageName := BuildTargetImageName(StartupConfig.SendDockerRegistry, imageName)
	log.Println("starting to pull image", tgtImageName)
	progressReader, err := apiClient.ImagePull(context.Background(), tgtImageName, pullOptions)
	if err != nil {
		log.Println("failed to pull image "+tgtImageName, err)
		return nil, err
	}
	defer progressReader.Close()
	io.Copy(os.Stdout, progressReader)

	log.Println("starting to save image", tgtImageName)
	return apiClient.ImageSave(context.Background(), []string{tgtImageName})
}

func BuildTargetImageName(registry, imageName string) string {
	if registry == "" {
		return imageName
	}
	return registry + "/" + imageName
}

func (a DockerArtifact) GetArtifactNameAndStream() (ArtifactNameAndStream, error) {
	stream, err := a.GetStream()
	if err != nil {
		log.Printf("failed to get Docker stream %v\n", err)
		return ArtifactNameAndStream{}, err
	}
	artifactDownloadName := a.GetDownloadFileName()
	return ArtifactNameAndStream{Name: artifactDownloadName, Stream: stream}, nil

}

func (a DockerArtifact) DeliverCleanup() error {
	// image must be deleted
	imageName := BuildTargetImageName(StartupConfig.SendDockerRegistry, a.GetOriginalResourceName())
	return cleanup(imageName)
}

func (a DockerArtifact) DeployCleanup() error {
	// image must be deleted
	imageName := BuildTargetImageName(StartupConfig.ReceiveDockerRegistry, a.GetOriginalResourceName())
	return cleanup(imageName)
}

func cleanup(imageName string) error {
	apiClient, err := client.NewClientWithOpts(client.WithVersion(DockerApiVersion))
	if err != nil {
		log.Println("failed to open docker api client", err)
		return err
	}
	defer apiClient.Close()

	_, err = apiClient.ImageRemove(context.Background(), imageName, image.RemoveOptions{})
	if err != nil {
		log.Printf("failed to remove image %s. error: %v\n", imageName, err)
		return err
	}
	log.Printf("removed image %s successfully\n", imageName)

	return nil
}

func (a DockerArtifact) GetType() ArtifactType {
	return DOCKER
}

func InitDockerClientApiVersion() {
	cmd := exec.Command("docker", "version")

	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	cmdOutput := out.String()
	log.Println("----------- `docker version` OUTPUT START -----------")
	log.Println("\n", cmdOutput)
	log.Println("----------- `docker version` OUTPUT END   -----------")
	if err != nil {
		log.Println("Execution of `docker version` command failed. See output for more details")
		log.Fatal(err)
	}

	lines := strings.Split(cmdOutput, "\n")
	serverLineMet := false
	for _, line := range lines {
		if !serverLineMet && strings.HasPrefix(line, "Server:") {
			serverLineMet = true
		}
		if serverLineMet && strings.HasPrefix(strings.Trim(line, " "), "API version") {
			regex := regexp.MustCompile(`(\d\.\d{1,}) (\(minimum version (\d\.\d{1,})\))+`)
			matches := regex.FindStringSubmatch(line)
			if len(matches) == 4 {
				log.Printf("Api Version = %s; Minimum Api version = %s\n", matches[1], matches[3])
			} else {
				log.Printf("Api Version = %s\n", matches[1])
			}
			DockerApiVersion = matches[1]
			return
		}
	}
	log.Println("Failed to get Server Api Version")
	log.Println("Look for `permission denied while trying to connect to the Docker daemon at unix:///var/run/docker.sock` in `docker version` output")
	log.Println("To fix this see Docker section in https://sdlc.go.rshbank.ru/confluence/pages/viewpage.action?pageId=308683890")
	log.Fatal("docker is unavailable")
}
