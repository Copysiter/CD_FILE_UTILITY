package cfg

import (
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func ReadInitConfig(filePath string) (*StartupConfig, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var startupConfig StartupConfig
	err = json.Unmarshal(file, &startupConfig)
	if err != nil {
		return nil, err
	}

	return &startupConfig, nil
}

type StartupConfig struct {
	StartupPort                   string `json:"port"`
	NFSPath                       string `json:"nfs_path"`
	SmbSharePath                  string `json:"smb_share_path,omitempty"`
	BufferSize                    string `json:"buffer_size"`
	ChunkSize                     string `json:"chunk_size"`
	EnableChunking                bool   `json:"enable_chunking"`
	ChunkingThreshold             string `json:"chunking_threshold"`
	Mode                          Mode   `json:"mode"`
	SendDockerEnabled             bool   `json:"send_docker_enabled,omitempty"`
	SendDockerRegistry            string `json:"send_docker_registry,omitempty"`
	SendDockerRegistryLogin       string `json:"send_docker_registry_login,omitempty"`
	SendDockerRegistryPassword    string `json:"send_docker_registry_password,omitempty"`
	SendNexusUrl                  string `json:"send_nexus_url,omitempty"`
	SendNexusLogin                string `json:"send_nexus_login,omitempty"`
	SendNexusPassword             string `json:"send_nexus_password,omitempty"`
	SendNexusPypiRepository       string `json:"send_nexus_pypi_repository,omitempty"`
	SendNexusHFRepository         string `json:"send_nexus_hf_repository,omitempty"`
	ReceiveDockerEnabled          bool   `json:"receive_docker_enabled,omitempty"`
	ReceiveDockerRegistry         string `json:"receive_docker_registry,omitempty"`
	ReceiveDockerRegistryLogin    string `json:"receive_docker_registry_login,omitempty"`
	ReceiveDockerRegistryPassword string `json:"receive_docker_registry_password,omitempty"`
	ReceivePypiEnabled            bool   `json:"receive_pypi_enabled,omitempty"`
	ReceiveHfEnabled              bool   `json:"receive_hf_enabled,omitempty"`
	ReceiveNexusUrl               string `json:"receive_nexus_url,omitempty"`
	ReceiveNexusLogin             string `json:"receive_nexus_login,omitempty"`
	ReceiveNexusPassword          string `json:"receive_nexus_password,omitempty"`
	ReceiveNexusPypiRepository    string `json:"receive_nexus_pypi_repository,omitempty"`
	ReceiveNexusHfRepository      string `json:"receive_nexus_hf_repository,omitempty"`
}

func (cfg *StartupConfig) RefineConfig() {
	if !strings.HasPrefix(cfg.StartupPort, ":") {
		log.Fatalln("startup port must start with ':', e.g. ':8080'")
	}
	_, defaultValue := cfg.GetBufferSize()
	if defaultValue {
		cfg.BufferSize = DEFAULT_BUFFER_SIZE_NAME
	}
	if cfg.Mode != CdSendMode {
		cfg.Mode = CdReceiveMode
	}
	cfg.BufferSize = strings.ToUpper(cfg.BufferSize)
	if cfg.Mode == CdReceiveMode && cfg.ReceiveDockerEnabled {
		if cfg.ReceiveDockerRegistry == "" {
			log.Fatalln("config key `receive_docker_registry` must be set!")
		}
		if cfg.ReceiveDockerRegistryLogin == "" {
			log.Fatalln("config key `receive_docker_login` must be set!")
		}
		if cfg.ReceiveDockerRegistryPassword == "" {
			log.Fatalln("config key `receive_docker_password` must be set!")
		}
	}
	if strings.Contains(cfg.SendNexusPassword, "#") {
		log.Println("config key `send_nexus_password` contains '#' symbol. It is better to be escaped with `%23`.")
		log.Println("For more details see https://github.com/jackc/pgx/issues/1285")
	}

	cfg.SendNexusUrl = strings.TrimSuffix(cfg.SendNexusUrl, "/")
}

func (cfg *StartupConfig) GetBufferSize() (retVal int, defaultValue bool) {
	bufferSizeStr := strings.ToUpper(strings.Trim(cfg.BufferSize, " "))
	kbRegex := regexp.MustCompile(`^(\d+)KB$`)
	subMatches := kbRegex.FindStringSubmatch(bufferSizeStr)
	if len(subMatches) > 0 {
		kilobytes, _ := strconv.Atoi(subMatches[1])
		bufferSize := 1024 * kilobytes
		if bufferSize > 0 {
			log.Println("Setting BufferSize to " + strconv.Itoa(bufferSize) + " bytes")
			return bufferSize, false
		} else {
			log.Println("Setting BufferSize to default value of " + strconv.Itoa(DEFAULT_BUFFER_SIZE) + " bytes")
			return DEFAULT_BUFFER_SIZE, true
		}
	}

	mbRegex := regexp.MustCompile(`^(\d+)MB$`)
	subMatches = mbRegex.FindStringSubmatch(bufferSizeStr)
	if len(subMatches) > 0 {
		megabytes, _ := strconv.Atoi(subMatches[1])
		bufferSize := 1024 * 1024 * megabytes
		if bufferSize > 0 {
			log.Println("Setting BufferSize to " + strconv.Itoa(bufferSize) + " bytes")
			return bufferSize, false
		} else {
			log.Println("Setting BufferSize to default value of " + strconv.Itoa(DEFAULT_BUFFER_SIZE) + " bytes")
			return DEFAULT_BUFFER_SIZE, true
		}
	}
	log.Println("Setting BufferSize to default value of " + strconv.Itoa(DEFAULT_BUFFER_SIZE) + " bytes")
	return DEFAULT_BUFFER_SIZE, true
}

const DEFAULT_BUFFER_SIZE = 5 * 1024 * 1024
const DEFAULT_BUFFER_SIZE_NAME = "5MB"

type Mode string

const (
	CdSendMode    Mode = "SEND"
	CdReceiveMode Mode = "RECEIVE"
)