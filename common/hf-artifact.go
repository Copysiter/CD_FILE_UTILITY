package common

import (
    "encoding/json"
    "errors"
    "fmt"
    "fts-cd-file-utility/cfg"
    "log"
    "net/http"
    "net/url"
    "strings"
)

// HfArtifact описывает путь к модели вида "Qwen/Qwen2.5-Coder-3B-Instruct".
// Например, в Nexus это лежит в репозитории "huggingface-hosted".
type HfArtifact struct {
    ModelName string
}

// Обозначим новый тип в enum ArtifactType.
// (См. ниже про правку structs.go)
func (a HfArtifact) GetType() ArtifactType {
    return HF
}

// Возвращает исходное имя ресурса (строка вида "Qwen/Qwen2.5-Coder-3B-Instruct").
func (a HfArtifact) GetOriginalResourceName() string {
    return a.ModelName
}

// buildNexusSearchHfUrl формирует URL для поиска модели в Nexus.
// По аналогии с pypi, но здесь мы используем "name=Qwen/Qwen2.5-Coder-3B-Instruct".
// Возможно, лучше завести отдельные поля в cfg.SendNexusHfRepository,
// но в примере переиспользуем send_nexus_pypi_repository.
func buildNexusSearchHfUrl(cfg *cfg.StartupConfig, artifact *HfArtifact) string {
    // Пример: GET /service/rest/v1/search?repository=huggingface-hosted&name=Qwen/Qwen2.5-Coder-3B-Instruct
    return fmt.Sprintf("%s/service/rest/v1/search?repository=%s&group=/%s",
        strings.TrimSuffix(cfg.SendNexusUrl, "/"),
        url.QueryEscape(cfg.SendNexusHFRepository), // или свой ключ (send_nexus_hf_repository)
        url.QueryEscape(artifact.ModelName),
    )
}

// Реализуем скачивание (по аналогии с pypi-artifact).
func (a HfArtifact) GetArtifactNameAndStream() (ArtifactNameAndStream, error) {
    searchUrl := buildNexusSearchHfUrl(&StartupConfig, &a)
    log.Println("Searching HuggingFace model with URL:", searchUrl)

    req, err := http.NewRequest("GET", searchUrl, nil)
    if err != nil {
        log.Printf("Failed to create HF model search request. err: %v\n", err)
        return ArtifactNameAndStream{}, err
    }
    req.SetBasicAuth(StartupConfig.SendNexusLogin, StartupConfig.SendNexusPassword)

    resp, err := HttpClient.Do(req)
    if err != nil {
        log.Printf("Failed HF model search request. err: %v\n", err)
        return ArtifactNameAndStream{}, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        msg := fmt.Sprintf("HF model search failed. Status code = %d", resp.StatusCode)
        log.Println(msg)
        return ArtifactNameAndStream{}, errors.New(msg)
    }

    // Парсим ответ (структура NexusSearchResponse уже есть в nexus-structs.go)
    parsedSearchResponse := new(NexusSearchResponse)
    err = json.NewDecoder(resp.Body).Decode(&parsedSearchResponse)
    if err != nil {
        log.Println("Failed to decode nexus search response:", err)
        return ArtifactNameAndStream{}, err
    }

    if len(parsedSearchResponse.Items) == 0 {
        msg := fmt.Sprintf("Model '%s' not found in Nexus huggingface-hosted", a.GetOriginalResourceName())
        log.Println(msg)
        return ArtifactNameAndStream{}, errors.New(msg)
    }

    // Берём первый item (или ищите нужный, если их несколько)
    packageItem := parsedSearchResponse.Items[0]
    if len(packageItem.Assets) == 0 {
        msg := fmt.Sprintf("No asset found for HF model '%s'", a.GetOriginalResourceName())
        log.Println(msg)
        return ArtifactNameAndStream{}, errors.New(msg)
    }

    downloadUrl := packageItem.Assets[0].DownloadUrl
    log.Println("downloadUrl =", downloadUrl)

    // Определяем имя файла
    downloadFileName, err := GetDownloadFileNameFromUrl(downloadUrl)
    if err != nil {
        return ArtifactNameAndStream{}, err
    }
    // Запрашиваем сам архив (zip или иной)
    downloadReq, err := http.NewRequest("GET", downloadUrl, nil)
    if err != nil {
        log.Println("Failed to create request for HF model download:", err)
        return ArtifactNameAndStream{}, err
    }
    downloadReq.SetBasicAuth(StartupConfig.SendNexusLogin, StartupConfig.SendNexusPassword)

    downloadResp, err := HttpClient.Do(downloadReq)
    if err != nil {
        log.Println("Failed to download HF model file:", err)
        return ArtifactNameAndStream{}, err
    }

    return ArtifactNameAndStream{Name: downloadFileName, Stream: downloadResp.Body}, nil
}

// Очистка на стороне SEND (если нужно).
func (a HfArtifact) DeliverCleanup() error {
    return nil
}

// Очистка на стороне RECEIVE (если нужно).
func (a HfArtifact) DeployCleanup() error {
    return nil
}