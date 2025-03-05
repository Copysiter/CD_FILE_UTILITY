package deploy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"fts-cd-file-utility/cfg"
	"fts-cd-file-utility/common"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/hirochachacha/go-smb2"
	"github.com/labstack/echo/v4"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func StartDockerDeployHandler(c echo.Context) error {
	jobId := c.Param("jobId")
	job := new(common.Job)
	if err := c.Bind(job); err != nil {
		return err
	}
	log.Println(jobId)

	jobFile, err := os.ReadFile(jobId + ".job")
	if err != nil {
		return err
	}

	// todo ivan should unmarshal using
	var dockerArtifact common.DockerArtifact
	var dockerJobStatus = common.JobStatus{Artifact: &dockerArtifact}
	err = json.Unmarshal(jobFile, &dockerJobStatus)
	if err != nil {
		return err
	}
	log.Printf("JobStatus = %+v\n", dockerJobStatus)
	log.Printf("dockerArtifact = %+v\n", dockerArtifact)

	imageFileName := filepath.Join(common.StartupConfig.NFSPath, dockerArtifact.GetDownloadFileName())
	err = loadImage(imageFileName, dockerArtifact)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, dockerJobStatus)
}

func LoadArtifacts(ctx context.Context, config *cfg.StartupConfig) {
	u, err := url.Parse(config.NFSPath)
	if err != nil {
		log.Printf("failed to parse NFSPath property %s. It is not valid url\n", config.NFSPath)
	}
	if strings.ToLower(u.Scheme) == "smb" {
		LoadArtifactsFromSmb(ctx, *u)
	} else {
		log.Println("Using local fileSystem is not supported. Use smb instead")
		// config.NFSPath = u.Path
		//log.Println("Using local fileSystem. NFSPath is", u.Path)
		// LoadArtifactsFs(ctx)
	}
}
func LoadArtifactsFs(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("stop loading artifacts since stopping application")
			return
		default:
			// log.Println("starting to read dir", common.StartupConfig.NFSPath, "to find new jobs")
			files, err := os.ReadDir(common.StartupConfig.NFSPath)
			if err != nil {
				log.Println("failed to read dir", common.StartupConfig.NFSPath, err)
				continue
			}
			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".job") {
					jobId := strings.Split(f.Name(), ".job")[0]
					log.Println("found jobFile", f.Name(), "with jobId =", jobId)
					jobFilePath := filepath.Join(common.StartupConfig.NFSPath, f.Name())
					jobFile, err := os.ReadFile(jobFilePath)
					if err != nil {
						log.Println("failed to read file", jobFilePath)
						continue
					}
					// todo ivan use artifact load base on artifact type
					var dockerArtifact common.DockerArtifact
					var dockerJobStatus = common.JobStatus{Artifact: &dockerArtifact}
					err = json.Unmarshal(jobFile, &dockerJobStatus)
					if err != nil {
						log.Println("failed to read json from file", jobFilePath)
						continue
					}
					log.Printf("JobStatus = %+v\n", dockerJobStatus)
					log.Printf("dockerArtifact = %+v\n", dockerArtifact)

					imageFileName := filepath.Join(common.StartupConfig.NFSPath, dockerArtifact.GetDownloadFileName())
					err = loadImage(imageFileName, dockerArtifact)
					if err != nil {
						log.Print("failed to load image", imageFileName, err)
						continue
					}

					// delete image
					err = os.Remove(imageFileName)
					if err != nil {
						log.Println("failed to remove image", imageFileName)
					}
					err = os.Remove(jobFilePath)
					if err != nil {
						log.Println("failed to remove job file", jobFilePath)
					}
					log.Println(jobId, "is successfully finished!")
					log.Println("image", dockerArtifact.ImageName, "is successfully loaded!")
				}
			}
		}
		time.Sleep(10 * time.Second)
	}
}

func loadFromSmb(ctx context.Context, u url.URL) {
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		log.Printf("failed to dial nfs %s. Error was %v", u.Host, err)
		return
	}
	defer conn.Close()

	password, passwordSet := u.User.Password()
	userAndDomain := strings.Split(u.User.Username(), "@")
	if len(userAndDomain) != 2 {
		// log.Println()
		panic(fmt.Sprintf("domain must be set. But username was %s", u.User.Username()))
	}
	user := userAndDomain[0]
	domain := userAndDomain[1]

	var initiator smb2.NTLMInitiator
	if passwordSet {
		initiator = smb2.NTLMInitiator{
			User:     user,
			Password: password,
			Domain:   domain,
		}
	} else {
		initiator = smb2.NTLMInitiator{
			User:   user,
			Domain: domain,
		}
	}

	d := &smb2.Dialer{
		Initiator: &initiator,
	}

	s, err := d.Dial(conn)
	if err != nil {
		log.Printf("failed to dial smb %s. Error was %v", u.Host, err)
		return
	}
	defer s.Logoff()

	shareName := buildShareName(u)
	// log.Println("mounting share", shareName)
	//fs, err := s.Mount("\\\\sgo-fc01-r13.go.rshbank.ru\\intech")
	fs, err := s.Mount(shareName)
	if err != nil {
		log.Printf("failed to mount %s. Error was %v", shareName, err)
		return
	}
	defer fs.Umount()

	// files, err := os.ReadDir(common.StartupConfig.NFSPath)
	// log.Println("")
	files, err := fs.ReadDir(common.StartupConfig.SmbSharePath)
	if err != nil {
		log.Println("failed to read dir", common.StartupConfig.SmbSharePath, err)
		return
	}
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".job") {
			jobId := strings.Split(f.Name(), ".job")[0]
			log.Println("found jobFile", f.Name(), "with jobId =", jobId)
			jobFilePath := filepath.Join(common.StartupConfig.SmbSharePath, f.Name())
			jobFile, err := fs.Open(jobFilePath)
			if err != nil {
				log.Println("failed to open job file", jobFilePath)
				continue
			}
			//jobFile, err := os.ReadFile(jobFilePath)
			jobFileContent, err := io.ReadAll(jobFile)
			if err != nil {
				log.Println("failed to read job file", jobFilePath)
				continue
			}
			var basicJobStatus = new(common.JobStatus)
			err = json.Unmarshal(jobFileContent, &basicJobStatus)
			
			// Проверяем, является ли артефакт фрагментированным
			isChunked, mergedFilePath, err := TryProcessChunkedArtifact(fs, jobFileContent, jobFilePath)
			if err != nil {
				log.Printf("Error processing chunked artifact: %v\n", err)
				continue
			}
			
			// Если это фрагментированный артефакт, используем объединенный файл
			if isChunked {
				log.Printf("Successfully merged chunks into file: %s\n", mergedFilePath)
				
				// Обновляем путь к артефакту
				basicJobStatus.ArtifactPath = mergedFilePath
				basicJobStatus.IsChunked = false
				
				// Записываем обновленный .job файл
				updatedJobContent, err := json.Marshal(basicJobStatus)
				if err == nil {
					jobFile, err := fs.Create(jobFilePath)
					if err == nil {
						jobFile.Write(updatedJobContent)
						jobFile.Close()
					}
				}
			}

			// if IsDockerArtifact(jobFileContent) {
			if basicJobStatus.ArtifactType == common.DOCKER {
				log.Println("docker artifact upload job found", jobFile)
				if !common.StartupConfig.ReceiveDockerEnabled {
					log.Println("docker artifact won't be processed since property `receive_docker_enabled` set to false")
					continue
				}
				var dockerArtifact common.DockerArtifact
				var dockerJobStatus = common.JobStatus{Artifact: &dockerArtifact}
				err = json.Unmarshal(jobFileContent, &dockerJobStatus)
				if err != nil {
					log.Println("failed to read json from file", jobFilePath)
					continue
				}
				log.Printf("JobStatus = %+v\n", dockerJobStatus)
				log.Printf("dockerArtifact = %+v\n", dockerArtifact)

				imageFileName := filepath.Join(common.StartupConfig.SmbSharePath, dockerJobStatus.ArtifactPath)
				err = smbLoadImage(imageFileName, dockerArtifact, fs)
				if err != nil {
					log.Print("failed to load image", imageFileName, err)
					continue
				}
				cleanUp(imageFileName, jobFilePath, fs)
				log.Println("image", dockerArtifact.ImageName, "is successfully loaded!")
				// } else if IsPypiArtifact(jobFileContent) {
			} else if basicJobStatus.ArtifactType == common.HF {
			    log.Println("hf artifact upload job found", jobFile)
			    if !common.StartupConfig.ReceiveHfEnabled {
					log.Println("huggingface artifact won't be processed since property `receive_hf_enabled` set to false")
					continue
				}
				var hfArtifact common.HfArtifact
				var hfJobStatus = common.JobStatus{Artifact: &hfArtifact}
				err = json.Unmarshal(jobFileContent, &hfJobStatus)
				if err != nil {
					log.Println("failed to read json from file", jobFilePath)
					continue
				}
				log.Printf("JobStatus = %+v\n", hfJobStatus)
				log.Printf("hfArtifact = %+v\n", hfArtifact)

				hfFileName := filepath.Join(common.StartupConfig.SmbSharePath, hfJobStatus.ArtifactPath)
				err = smbUploadHfModel(hfFileName, hfJobStatus.ArtifactPath, hfArtifact, fs)
				if err != nil {
					log.Printf("failed to load huggingface model %s. Err: %v\n", hfFileName, err)
					continue
				}
				cleanUp(hfFileName, jobFilePath, fs)
				log.Println("model", hfArtifact.ModelName, "is successfully loaded!")
			} else if basicJobStatus.ArtifactType == common.PYPI {
				log.Println("pypi artifact upload job found", jobFile)
				if !common.StartupConfig.ReceivePypiEnabled {
					log.Println("pypi artifact won't be processed since property `receive_pypi_enabled` set to false")
					continue
				}
				var pypiArtifact common.PypiArtifact
				var pypiJobStatus = common.JobStatus{Artifact: &pypiArtifact}
				err = json.Unmarshal(jobFileContent, &pypiJobStatus)
				if err != nil {
					log.Println("failed to read json from file", jobFilePath)
					continue
				}
				log.Printf("JobStatus = %+v\n", pypiJobStatus)
				log.Printf("pypiArtifact = %+v\n", pypiArtifact)

				pypiFileName := filepath.Join(common.StartupConfig.SmbSharePath, pypiJobStatus.ArtifactPath)
				err = smbUploadPypiPackage(pypiFileName, pypiJobStatus.ArtifactPath, pypiArtifact, fs)
				if err != nil {
					log.Printf("failed to load pypi package %s. Err: %v\n", pypiFileName, err)
					continue
				}
				cleanUp(pypiFileName, jobFilePath, fs)
				log.Println("package", pypiArtifact.PackageName, "is successfully loaded!")
			} else {
				continue
			}
			log.Println(jobId, "is successfully finished!")
		}
	}
	return
}

func cleanUp(artifactFileName, jobFilePath string, fs *smb2.Share) {
	err := fs.Remove(artifactFileName)
	if err != nil {
		log.Println("failed to remove artifact file", artifactFileName)
	}
	err = fs.Remove(jobFilePath)
	if err != nil {
		log.Println("failed to remove job file", jobFilePath)
		log.Println(err)
	}
}

func IsDockerArtifact(jobFileContent []byte) bool {
	var dockerArtifact common.DockerArtifact
	var dockerJobStatus = common.JobStatus{Artifact: &dockerArtifact}
	err := json.Unmarshal(jobFileContent, &dockerJobStatus)
	return err == nil && dockerArtifact.ImageName != ""
}

func IsPypiArtifact(jobFileContent []byte) bool {
	var pypiArtifact common.PypiArtifact
	var pypiJobStatus = common.JobStatus{Artifact: &pypiArtifact}
	err := json.Unmarshal(jobFileContent, &pypiJobStatus)
	return err == nil && pypiArtifact.PackageName != ""
}

func IsHfArtifact(jobFileContent []byte) bool {
	var hfArtifact common.HfArtifact
	var hfJobStatus = common.JobStatus{Artifact: &hfArtifact}
	err := json.Unmarshal(jobFileContent, &hfJobStatus)
	return err == nil && hfArtifact.ModelName != ""
}

func buildShareName(u url.URL) string {
	host, _, _ := net.SplitHostPort(u.Host)
	share := strings.ReplaceAll(u.Path, "/", "")
	return "\\\\" + host + "\\" + share
}

func LoadArtifactsFromSmb(ctx context.Context, u url.URL) {
	for {
		select {
		case <-ctx.Done():
			log.Println("stop loading artifacts since stopping application")
			return
		default:
			loadFromSmb(ctx, u)
		}
		time.Sleep(10 * time.Second)
	}
}

func loadImage(imageFileName string, artifact common.DockerArtifact) error {
	apiClient, err := client.NewClientWithOpts(client.WithVersion(common.DockerApiVersion))
	if err != nil {
		log.Println("failed to open docker api client", err)
		return err
	}
	defer apiClient.Close()

	log.Println("starting to load image", imageFileName)

	//imageFileName := "/home/GO/raisa/image.docker"
	imageFile, err := os.OpenFile(imageFileName, os.O_RDONLY, 0644)
	defer imageFile.Close()
	if err != nil {
		log.Println("failed to open image", imageFileName, err)
		return err
	}
	load, err := apiClient.ImageLoad(context.Background(), imageFile, false)
	if err != nil {
		body, errLoad := io.ReadAll(load.Body)
		if errLoad != nil {
			log.Println("failed to read loadResponse!", errLoad)
		} else {
			log.Println(string(body))
		}
		log.Println("failed to load image", imageFileName, err)
		return err
	} else {
		body, errLoad := io.ReadAll(load.Body)
		if errLoad != nil {
			log.Println("failed to read loadResponse!", errLoad)
		} else {
			log.Println(string(body))
		}
	}
	defer load.Body.Close()

	receiveTag := common.BuildTargetImageName(common.StartupConfig.ReceiveDockerRegistry, artifact.ImageName)
	sendImage := common.BuildTargetImageName(common.StartupConfig.SendDockerRegistry, artifact.ImageName)
	log.Println("starting to tag image", sendImage, "with tag", receiveTag)
	err = apiClient.ImageTag(context.Background(), sendImage, receiveTag)
	if err != nil {
		log.Printf("failed to tag image artifact %s with tag %s. error: %v\n", sendImage, receiveTag, err)
		return err
	}
	log.Println("starting to push image", receiveTag)

	authConfig := registry.AuthConfig{Username: common.StartupConfig.ReceiveDockerRegistryLogin, Password: common.StartupConfig.ReceiveDockerRegistryPassword, ServerAddress: common.StartupConfig.ReceiveDockerRegistry}
	authConfigBytes, err := json.Marshal(authConfig)
	if err != nil {
		log.Printf("failed to marshal auth config for push options. error: %v\n", err)
		return err
	}
	authConfigEncoded := base64.URLEncoding.EncodeToString(authConfigBytes)
	progressReader, err := apiClient.ImagePush(context.Background(), receiveTag, image.PushOptions{RegistryAuth: authConfigEncoded})
	defer progressReader.Close()
	io.Copy(os.Stdout, progressReader)
	if err != nil {
		log.Printf("failed to push image %s. error: %v\n", receiveTag, err)
		return err
	}

	err = artifact.DeployCleanup()
	if err != nil {
		log.Printf("failed to remove image %s. Error: %v\n", receiveTag, err)
	}
	return nil
}

func smbUploadPypiPackage(pypiFilePath, artifactFileName string, artifact common.PypiArtifact, fs *smb2.Share) error {
	pypiFromFile, err := fs.OpenFile(pypiFilePath, os.O_RDONLY, 0644)
	if err != nil {
		log.Println("failed to open image", pypiFilePath, err)
		return err
	}

	pypiTgtFile, err := os.Create(artifactFileName)
	if err != nil {
		log.Println("failed to create tgtFile")
		return err
	}
	_, err = io.Copy(pypiTgtFile, pypiFromFile)
	if err != nil {
		log.Println("failed to copy file %s to %s. Error: %v", pypiFilePath, pypiTgtFile.Name(), err)
		return err
	}
	// twine upload --repository-url http://10.7.86.10:8081/repository/pypi-hosted/ -u USER -p PASSWORD Hello_World_Package-0.1.3-py2.py3-none-any.whl
	cmd := exec.Command("twine", "upload",
		"--repository-url", buildNexusPypiRepoName(),
		"-u", common.StartupConfig.ReceiveNexusLogin,
		"-p", common.StartupConfig.ReceiveNexusPassword,
		artifactFileName)

	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	cmdOutput := out.String()
	log.Println("----------- `twine upload` OUTPUT START -----------")
	log.Println("\n", cmdOutput)
	log.Println("----------- `twine upload` OUTPUT END   -----------")

	return nil
}

func smbUploadHfModel(hfFilePath, artifactFileName string, artifact common.HfArtifact, fs *smb2.Share) error {
	hfFromFile, err := fs.OpenFile(hfFilePath, os.O_RDONLY, 0644)
	if err != nil {
		log.Println("failed to open file", hfFilePath, err)
		return err
	}
	defer hfFromFile.Close()

	hfTgtFile, err := os.Create(artifactFileName)
	if err != nil {
		log.Println("failed to create target file", artifactFileName, err)
		return err
	}
	defer hfTgtFile.Close()

	_, err = io.Copy(hfTgtFile, hfFromFile)
	if err != nil {
		log.Println("failed to copy file from", hfFilePath, "to", artifactFileName, err)
		return err
	}

	nexusURL := buildNexusHfRepoName()
	uploadURL := fmt.Sprintf("%s%s/%s", nexusURL, artifact.ModelName, filepath.Base(artifactFileName))

	uploadFile, err := os.Open(artifactFileName)
	if err != nil {
		log.Println("failed to open file for upload", artifactFileName, err)
		return err
	}
	defer uploadFile.Close()

	info, err := uploadFile.Stat()
	if err != nil {
		log.Println("failed to get file info", artifactFileName, err)
		return err
	}
	fileSize := info.Size()

	req, err := http.NewRequest(http.MethodPut, uploadURL, uploadFile)
	if err != nil {
		log.Println("failed to create request", err)
		return err
	}

	req.ContentLength = fileSize

	req.SetBasicAuth(common.StartupConfig.ReceiveNexusLogin, common.StartupConfig.ReceiveNexusPassword)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("upload request failed", err)
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("failed to read response", err)
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("upload failed with status %d: %s\n", resp.StatusCode, string(respBody))
		return fmt.Errorf("upload failed with status %d", resp.StatusCode)
	}

	log.Printf("File %s uploaded successfully to %s\n", artifactFileName, uploadURL)
	return nil
}

func buildNexusPypiRepoName() string {
	if strings.HasSuffix(common.StartupConfig.ReceiveNexusPypiRepository, "/") {
		return common.StartupConfig.ReceiveNexusUrl + "/repository/" + common.StartupConfig.ReceiveNexusPypiRepository
	}
	return common.StartupConfig.ReceiveNexusUrl + "/repository/" + common.StartupConfig.ReceiveNexusPypiRepository + "/"
}

func buildNexusHfRepoName() string {
	if strings.HasSuffix(common.StartupConfig.ReceiveNexusHfRepository, "/") {
		return common.StartupConfig.ReceiveNexusUrl + "/repository/" + common.StartupConfig.ReceiveNexusHfRepository
	}
	return common.StartupConfig.ReceiveNexusUrl + "/repository/" + common.StartupConfig.ReceiveNexusHfRepository + "/"
}

func smbLoadImage(imageFileName string, artifact common.DockerArtifact, fs *smb2.Share) error {
	apiClient, err := client.NewClientWithOpts(client.WithVersion(common.DockerApiVersion))
	if err != nil {
		log.Println("failed to open docker api client", err)
		return err
	}
	defer apiClient.Close()

	log.Println("starting to load image", imageFileName)

	//imageFileName := "/home/GO/raisa/image.docker"
	imageFile, err := fs.OpenFile(imageFileName, os.O_RDONLY, 0644)
	// imageFile, err := imageReader(imageFileName)
	// defer imageFile.Close()
	if err != nil {
		log.Println("failed to open image", imageFileName, err)
		return err
	}
	load, err := apiClient.ImageLoad(context.Background(), imageFile, false)
	if err != nil {
		body, errLoad := io.ReadAll(load.Body)
		if errLoad != nil {
			log.Println("failed to read loadResponse!", errLoad)
		} else {
			log.Println(string(body))
		}
		log.Println("failed to load image", imageFileName, err)
		return err
	} else {
		body, errLoad := io.ReadAll(load.Body)
		if errLoad != nil {
			log.Println("failed to read loadResponse!", errLoad)
		} else {
			log.Println(string(body))
		}
	}
	defer load.Body.Close()

	receiveTag := common.BuildTargetImageName(common.StartupConfig.ReceiveDockerRegistry, artifact.ImageName)
	sendImage := common.BuildTargetImageName(common.StartupConfig.SendDockerRegistry, artifact.ImageName)
	log.Println("starting to tag image", sendImage, "with tag", receiveTag)
	err = apiClient.ImageTag(context.Background(), sendImage, receiveTag)
	if err != nil {
		log.Printf("failed to tag image artifact %s with tag %s. error: %v\n", sendImage, receiveTag, err)
		return err
	}
	log.Println("starting to push image", receiveTag)

	authConfig := registry.AuthConfig{Username: common.StartupConfig.ReceiveDockerRegistryLogin, Password: common.StartupConfig.ReceiveDockerRegistryPassword, ServerAddress: common.StartupConfig.ReceiveDockerRegistry}
	authConfigBytes, err := json.Marshal(authConfig)
	if err != nil {
		log.Printf("failed to marshal auth config for push options. error: %v\n", err)
		return err
	}
	authConfigEncoded := base64.URLEncoding.EncodeToString(authConfigBytes)
	progressReader, err := apiClient.ImagePush(context.Background(), receiveTag, image.PushOptions{RegistryAuth: authConfigEncoded})
	defer progressReader.Close()
	io.Copy(os.Stdout, progressReader)
	if err != nil {
		log.Printf("failed to push image %s. error: %v\n", receiveTag, err)
		return err
	}

	err = artifact.DeployCleanup()
	if err != nil {
		log.Printf("failed to remove image %s. Error: %v\n", receiveTag, err)
	}
	return nil
}


func connectToSmb2(sharePath url.URL) (*net.Conn, error) {
    //Implementation for connecting to SMB share.  Replace with actual logic.
    conn, err := net.Dial("tcp", sharePath.Host)
    return &conn, err
}

func mountSmbShare(conn *net.Conn, sharePath url.URL) (*smb2.Share, error) {
    //Implementation for mounting SMB share. Replace with actual logic.  Uses smb2 package.
    password, passwordSet := sharePath.User.Password()
    userAndDomain := strings.Split(sharePath.User.Username(), "@")
    if len(userAndDomain) != 2 {
        panic(fmt.Sprintf("domain must be set. But username was %s", sharePath.User.Username()))
    }
    user := userAndDomain[0]
    domain := userAndDomain[1]

    var initiator smb2.NTLMInitiator
    if passwordSet {
        initiator = smb2.NTLMInitiator{
            User:     user,
            Password: password,
            Domain:   domain,
        }
    } else {
        initiator = smb2.NTLMInitiator{
            User:   user,
            Domain: domain,
        }
    }

    d := &smb2.Dialer{
        Initiator: &initiator,
    }

    s, err := d.Dial(*conn)
    if err != nil {
        return nil, err
    }
    shareName := buildShareName(sharePath)
    fs, err := s.Mount(shareName)
    return fs, err

}


// TryProcessChunkedArtifact is implemented in chank-utils.go

func LoadDockerArtifactFromFile(filePath string, artifact common.DockerArtifact) error {
	//Implementation to load Docker artifact from a given file path. Replace with actual logic.
	return nil
}

func LoadPypiArtifactFromFile(filePath string, artifact common.PypiArtifact) error {
	//Implementation to load PyPI artifact from a given file path. Replace with actual logic.
	return nil
}

func LoadHfArtifactFromFile(filePath string, artifact common.HfArtifact) error {
	//Implementation to load Hugging Face artifact from a given file path. Replace with actual logic.
	return nil
}

func smbLoadDockerArtifact(dockerFileName string, artifact common.DockerArtifact, fs *smb2.Share) error {
	//Implementation for loading docker artifact from SMB.
	apiClient, err := client.NewClientWithOpts(client.WithVersion(common.DockerApiVersion))
	if err != nil {
		log.Println("failed to open docker api client", err)
		return err
	}
	defer apiClient.Close()

	log.Println("starting to load image", dockerFileName)

	imageFile, err := fs.OpenFile(dockerFileName, os.O_RDONLY, 0644)
	if err != nil {
		log.Println("failed to open image", dockerFileName, err)
		return err
	}
	defer imageFile.Close()

	load, err := apiClient.ImageLoad(context.Background(), imageFile, false)
	if err != nil {
		body, errLoad := io.ReadAll(load.Body)
		if errLoad != nil {
			log.Println("failed to read loadResponse!", errLoad)
		} else {
			log.Println(string(body))
		}
		log.Println("failed to load image", dockerFileName, err)
		return err
	} else {
		body, errLoad := io.ReadAll(load.Body)
		if errLoad != nil {
			log.Println("failed to read loadResponse!", errLoad)
		} else {
			log.Println(string(body))
		}
	}
	defer load.Body.Close()

	receiveTag := common.BuildTargetImageName(common.StartupConfig.ReceiveDockerRegistry, artifact.ImageName)
	sendImage := common.BuildTargetImageName(common.StartupConfig.SendDockerRegistry, artifact.ImageName)
	log.Println("starting to tag image", sendImage, "with tag", receiveTag)
	err = apiClient.ImageTag(context.Background(), sendImage, receiveTag)
	if err != nil {
		log.Printf("failed to tag image artifact %s with tag %s. error: %v\n", sendImage, receiveTag, err)
		return err
	}
	log.Println("starting to push image", receiveTag)

	authConfig := registry.AuthConfig{Username: common.StartupConfig.ReceiveDockerRegistryLogin, Password: common.StartupConfig.ReceiveDockerRegistryPassword, ServerAddress: common.StartupConfig.ReceiveDockerRegistry}
	authConfigBytes, err := json.Marshal(authConfig)
	if err != nil {
		log.Printf("failed to marshal auth config for push options. error: %v\n", err)
		return err
	}
	authConfigEncoded := base64.URLEncoding.EncodeToString(authConfigBytes)
	progressReader, err := apiClient.ImagePush(context.Background(), receiveTag, image.PushOptions{RegistryAuth: authConfigEncoded})
	defer progressReader.Close()
	io.Copy(os.Stdout, progressReader)
	if err != nil {
		log.Printf("failed to push image %s. error: %v\n", receiveTag, err)
		return err
	}

	err = artifact.DeployCleanup()
	if err != nil {
		log.Printf("failed to remove image %s. Error: %v\n", receiveTag, err)
	}
	return nil
}


func smbLoadPypiArtifact(pypiFileName string, artifact common.PypiArtifact, fs *smb2.Share) error {
	//Implementation for loading pypi artifact from SMB.
	pypiFromFile, err := fs.OpenFile(pypiFileName, os.O_RDONLY, 0644)
	if err != nil {
		log.Println("failed to open image", pypiFileName, err)
		return err
	}

	pypiTgtFile, err := os.Create(pypiFileName) //Using the same filename for simplicity, adjust if needed.
	if err != nil {
		log.Println("failed to create tgtFile")
		return err
	}
	defer pypiTgtFile.Close()

	_, err = io.Copy(pypiTgtFile, pypiFromFile)
	if err != nil {
		log.Println("failed to copy file %s to %s. Error: %v", pypiFileName, pypiTgtFile.Name(), err)
		return err
	}
	defer pypiFromFile.Close()

	cmd := exec.Command("twine", "upload",
		"--repository-url", buildNexusPypiRepoName(),
		"-u", common.StartupConfig.ReceiveNexusLogin,
		"-p", common.StartupConfig.ReceiveNexusPassword,
		pypiFileName)

	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	cmdOutput := out.String()
	log.Println("----------- `twine upload` OUTPUT START -----------")
	log.Println("\n", cmdOutput)
	log.Println("----------- `twine upload` OUTPUT END   -----------")

	return nil
}