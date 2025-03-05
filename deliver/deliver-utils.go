package deliver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"fts-cd-file-utility/common"
	"github.com/labstack/echo/v4"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var jobStatusMap = JobStatusMap{
	JobStatusMap: make(map[string]common.JobStatus),
	Lock:         sync.RWMutex{},
}

var latestJob string

func StartDockerCdHandlerWithJobId(c echo.Context) error {
	jobId := c.Param("jobId")
	return startDockerJob(jobId, c)
}

func StartPypiCdHandler(c echo.Context) error {
	jobId := generateJobId()
	return startPypiJob(jobId, c)
}

func StartHfCdHandler(c echo.Context) error {
	jobId := generateJobId()
	return startHfJob(jobId, c)
}

func StartDockerCdHandler(c echo.Context) error {
	jobId := generateJobId()
	return startDockerJob(jobId, c)
}

func generateJobId() string {
	return time.Now().Format("20060102150405")
}

func startDockerJob(jobId string, c echo.Context) error {
	job := new(common.Job)
	if err := c.Bind(job); err != nil {
		return err
	}
	latestJob = jobId
	go startCd(jobId, common.DockerArtifact{
		ImageName: job.Artifact,
	})
	return c.JSON(http.StatusCreated, job)
}

func startPypiJob(jobId string, c echo.Context) error {
	job := new(common.PypiJob)
	if err := c.Bind(job); err != nil {
		return err
	}
	latestJob = jobId
	go startCd(jobId, common.PypiArtifact{
		PackageName: job.Artifact,
		Version:     job.Version,
	})
	return c.JSON(http.StatusCreated, job)
}

func startHfJob(jobId string, c echo.Context) error {
	job := new(common.HfJob)
	if err := c.Bind(job); err != nil {
		return err
	}
	latestJob = jobId
	go startCd(jobId, common.HfArtifact{
		ModelName: job.Artifact,
	})
	return c.JSON(http.StatusCreated, job)
}

func getBaseFilePath(path string) (string, error) {
	u, err := url.Parse(path)
	if err != nil {
		// return "", errors.New(fmt.Sprintf("failed to parse path as url. '%s' is not valid url\n", path))
		// todo ivan remove me later
		return "D:\\tmp\\pypi", nil
	}
	if strings.ToLower(u.Scheme) == "fs" {
		return u.Path, nil
	} else if strings.ToLower(u.Scheme) == "smb" {
		return "", errors.New("smb protocol is not supported in SEND mode")
	} else {
		return "", errors.New(fmt.Sprintf("unknown protocol %s", u.Scheme))
	}
}

func startCd(jobId string, artifact common.Artifact) {
	fsPath, err := getBaseFilePath(common.StartupConfig.NFSPath)
	if err != nil {
		log.Printf("failed to get path to store artifacts since %v", err)
		return
	}
	jobStatusMap.SetJobStatus(jobId, common.JobStatus{Artifact: artifact, ArtifactType: artifact.GetType(), Status: common.DOWNLOADING, StatusDttm: time.Now()})
	tempFilename := jobId + ".tmp"

	artifactNameAndStream, err := artifact.GetArtifactNameAndStream()
	if err != nil {
		log.Println("failed to get artifact", artifact, "stream", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{Artifact: artifact, Status: common.DOWNLOADING_FAILED, ArtifactPath: artifactNameAndStream.Name, StatusDttm: time.Now()})
		return
	}
	defer artifactNameAndStream.Stream.Close()

	tmpFilePath := filepath.Join(fsPath, tempFilename)
	tgtFilePath := filepath.Join(fsPath, artifactNameAndStream.Name)

	// Определим, нужна ли фрагментация
	useChunking := common.StartupConfig.EnableChunking

	// Если включена фрагментация, проверим размер для порога фрагментации
	chunkingThresholdStr := common.StartupConfig.ChunkingThreshold
	var chunkingThreshold int64 = 100 * 1024 * 1024 // 100MB по умолчанию
	if chunkingThresholdStr != "" {
		chunkingThreshold = common.CheckChunkSize(chunkingThresholdStr)
	}

	// Проверка свободного места на диске
	var stat syscall.Statfs_t
	if err := syscall.Statfs(fsPath, &stat); err == nil {
		// Получаем свободное место в байтах
		freeSpace := stat.Bavail * uint64(stat.Bsize)
		log.Printf("Free space on %s: %d bytes\n", fsPath, freeSpace)

		// Если включена фрагментация и свободного места меньше порога, используем фрагментацию
		if useChunking && freeSpace < uint64(chunkingThreshold) {
			downloadWithChunking(jobId, artifact, artifactNameAndStream, fsPath)
			return
		}
	} else {
		log.Printf("Warning: Could not check free space: %v\n", err)
	}

	// Стандартная загрузка без фрагментации
	tmpFile, err := os.Create(tmpFilePath)
	if err != nil {
		log.Printf("failed to create tmp file %s. Error: %v\n", tmpFilePath, err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{Artifact: artifact, Status: common.DOWNLOADING_FAILED, ArtifactPath: artifactNameAndStream.Name, StatusDttm: time.Now()})
		return
	}
	defer tmpFile.Close()

	bufferSize, _ := common.StartupConfig.GetBufferSize()
	buf := make([]byte, bufferSize)
	var downloaded int64
	for {
		n, err := artifactNameAndStream.Stream.Read(buf)
		if n > 0 {
			_, err := tmpFile.Write(buf[:n])
			if err != nil {
				log.Printf("Error while writing to tmp file: %v\n", err)
				tmpFile.Close()
				os.Remove(tmpFilePath) // Удаляем неполный временный файл
				jobStatusMap.SetJobStatus(jobId, common.JobStatus{Artifact: artifact, Status: common.DOWNLOADING_FAILED, ArtifactPath: artifactNameAndStream.Name, StatusDttm: time.Now()})

				// Если ошибка связана с нехваткой места и фрагментация разрешена, пробуем фрагментацию
				if strings.Contains(err.Error(), "no space") && useChunking {
					log.Printf("Not enough space for full download, switching to chunking mode\n")
					downloadWithChunking(jobId, artifact, artifactNameAndStream, fsPath)
					return
				}
				return
			}
			downloaded += int64(n)
		}
		if err != nil {
			if err == io.EOF {
				log.Printf("Job - %s:Downloading %s 100%\n", jobId, artifactNameAndStream.Name)
				break
			}
			log.Printf("Error while downloading: %v\n", err)
			tmpFile.Close()
			os.Remove(tmpFilePath) // Удаляем неполный временный файл
			jobStatusMap.SetJobStatus(jobId, common.JobStatus{Artifact: artifact, Status: common.DOWNLOADING_FAILED, ArtifactPath: artifactNameAndStream.Name, StatusDttm: time.Now()})
			return
		}
	}
	tmpFile.Close()
	err = os.Rename(tmpFilePath, tgtFilePath)
	if err != nil {
		log.Printf("Error while renaming tmp file: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{Artifact: artifact, Status: common.DOWNLOADING_FAILED, ArtifactPath: artifactNameAndStream.Name, StatusDttm: time.Now()})
		return
	}
	successJobStatus := common.JobStatus{Status: common.DOWNLOADING_DONE, Artifact: artifact, ArtifactType: artifact.GetType(), ArtifactPath: artifactNameAndStream.Name, StatusDttm: time.Now()}
	err = WriteMeta(fsPath, jobId, successJobStatus)
	if err != nil {
		log.Printf("failed to write meta file: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{Artifact: artifact, Status: common.META_WRITING_FAILED, ArtifactPath: artifactNameAndStream.Name, StatusDttm: time.Now()})
		return
	}
	err = artifact.DeliverCleanup()
	if err != nil {
		log.Printf("failed cleanup for artifact %+v. Error: %v\n", artifact, err)
	}
	jobStatusMap.SetJobStatus(jobId, successJobStatus)
}

// downloadWithChunking загружает файл по частям
func downloadWithChunking(jobId string, artifact common.Artifact, artifactNameAndStream common.ArtifactNameAndStream, fsPath string) {
	chunkSize := common.CheckChunkSize(common.StartupConfig.ChunkSize)
	log.Printf("Starting chunked download for %s with chunk size %d bytes\n", artifactNameAndStream.Name, chunkSize)

	// Обновляем статус
	jobStatusMap.SetJobStatus(jobId, common.JobStatus{
		Artifact:     artifact,
		ArtifactType: artifact.GetType(),
		Status:       common.CHUNKED,
		ArtifactPath: artifactNameAndStream.Name,
		StatusDttm:   time.Now(),
	})

	// Создаем директорию для фрагментов
	chunkDir := filepath.Join(fsPath, "chunks_"+jobId)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		log.Printf("Error creating chunk directory: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{
			Artifact:     artifact,
			Status:       common.DOWNLOADING_FAILED,
			ArtifactPath: artifactNameAndStream.Name,
			StatusDttm:   time.Now(),
		})
		return
	}

	// Начинаем загрузку фрагментов
	bufferSize, _ := common.StartupConfig.GetBufferSize()
	buf := make([]byte, bufferSize)

	chunkIndex := 0
	var totalDownloaded int64
	var currentChunkSize int64

	// Создаем манифест
	manifest := &common.FileManifest{
		OriginalFileName: artifactNameAndStream.Name,
		TotalSize:        0, // Пока неизвестно
		ChunkCount:       0, // Пока неизвестно
		Chunks:           make([]common.FileChunk, 0),
		Completed:        false,
	}

	// Создаем первый фрагмент
	chunkName := fmt.Sprintf("%s%d_%s", common.ChunkPrefix, chunkIndex, artifactNameAndStream.Name)
	chunkPath := filepath.Join(chunkDir, chunkName)
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		log.Printf("Error creating chunk file: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{
			Artifact:     artifact,
			Status:       common.DOWNLOADING_FAILED,
			ArtifactPath: artifactNameAndStream.Name,
			StatusDttm:   time.Now(),
		})
		return
	}

	//Create temporary file for hash calculation
	tempFullFilePath := filepath.Join(fsPath, "temp_"+jobId+"_"+artifactNameAndStream.Name)
	tempFullFile, err := os.Create(tempFullFilePath)
	if err != nil {
		log.Printf("Error creating temp file for hash: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{
			Artifact:     artifact,
			Status:       common.DOWNLOADING_FAILED,
			ArtifactPath: artifactNameAndStream.Name,
			StatusDttm:   time.Now(),
		})
		return
	}
	defer tempFullFile.Close()
	defer os.Remove(tempFullFilePath)


	for {
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{
			Artifact:     artifact,
			ArtifactType: artifact.GetType(),
			Status:       common.CHUNK_DOWNLOADING,
			ArtifactPath: artifactNameAndStream.Name,
			StatusDttm:   time.Now(),
		})

		n, err := artifactNameAndStream.Stream.Read(buf)
		if n > 0 {
			// Проверяем, не превышен ли размер фрагмента
			if currentChunkSize+int64(n) > chunkSize {
				// Закрываем текущий фрагмент
				chunkFile.Close()

				// Добавляем информацию о фрагменте в манифест
				manifest.Chunks = append(manifest.Chunks, common.FileChunk{
					Index:     chunkIndex,
					FileName:  chunkName,
					Size:      currentChunkSize,
					TotalSize: totalDownloaded + int64(n),
					IsLast:    false,
				})

				// Создаем новый фрагмент
				chunkIndex++
				chunkName = fmt.Sprintf("%s%d_%s", common.ChunkPrefix, chunkIndex, artifactNameAndStream.Name)
				chunkPath = filepath.Join(chunkDir, chunkName)

				chunkFile, err = os.Create(chunkPath)
				if err != nil {
					log.Printf("Error creating next chunk file: %v\n", err)
					jobStatusMap.SetJobStatus(jobId, common.JobStatus{
						Artifact:     artifact,
						Status:       common.DOWNLOADING_FAILED,
						ArtifactPath: artifactNameAndStream.Name,
						StatusDttm:   time.Now(),
					})
					return
				}

				currentChunkSize = 0
			}

			// Записываем данные во фрагмент
			_, err := chunkFile.Write(buf[:n])
			if err != nil {
				chunkFile.Close()
				log.Printf("Error writing to chunk file: %v\n", err)
				jobStatusMap.SetJobStatus(jobId, common.JobStatus{
					Artifact:     artifact,
					Status:       common.DOWNLOADING_FAILED,
					ArtifactPath: artifactNameAndStream.Name,
					StatusDttm:   time.Now(),
				})
				return
			}

			// Также пишем во временный полный файл для расчёта хеш-суммы
			if tempFullFile != nil {
				_, err := tempFullFile.Write(buf[:n])
				if err != nil {
					log.Printf("Warning: Error writing to temp file for hash: %v\n", err)
					// Не прерываем загрузку при ошибке с временным файлом
					tempFullFile.Close()
					tempFullFile = nil
					os.Remove(tempFullFilePath)
				}
			}

			currentChunkSize += int64(n)
			totalDownloaded += int64(n)
		}

		if err != nil {
			if err == io.EOF {
				log.Printf("Job - %s: Downloading %s 100%%\n", jobId, artifactNameAndStream.Name)
				break
			}
			chunkFile.Close()
			log.Printf("Error during download: %v\n", err)
			jobStatusMap.SetJobStatus(jobId, common.JobStatus{
				Artifact:     artifact,
				Status:       common.DOWNLOADING_FAILED,
				ArtifactPath: artifactNameAndStream.Name,
				StatusDttm:   time.Now(),
			})
			return
		}
	}

	// Закрываем последний фрагмент и добавляем в манифест
	chunkFile.Close()
	manifest.Chunks = append(manifest.Chunks, common.FileChunk{
		Index:     chunkIndex,
		FileName:  chunkName,
		Size:      currentChunkSize,
		TotalSize: totalDownloaded,
		IsLast:    true,
	})

	// Обновляем общие данные манифеста
	manifest.TotalSize = totalDownloaded
	manifest.ChunkCount = chunkIndex + 1

	// Сохраняем манифест
	manifestPath := filepath.Join(chunkDir, artifactNameAndStream.Name+common.ManifestSuffix)
	manifestFile, err := os.Create(manifestPath)
	if err != nil {
		log.Printf("Error creating manifest file: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{
			Artifact:     artifact,
			Status:       common.DOWNLOADING_FAILED,
			ArtifactPath: artifactNameAndStream.Name,
			StatusDttm:   time.Now(),
		})
		return
	}
	defer manifestFile.Close()

	encoder := json.NewEncoder(manifestFile)
	err = encoder.Encode(manifest)
	if err != nil {
		log.Printf("Error writing manifest: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{
			Artifact:     artifact,
			Status:       common.DOWNLOADING_FAILED,
			ArtifactPath: artifactNameAndStream.Name,
			StatusDttm:   time.Now(),
		})
		return
	}

	//Calculate hash of temporary file
	hash := calculateSHA256(tempFullFilePath)
	manifest.Hash = hash

	// Создаем статус задания с информацией о фрагментах
	successJobStatus := common.JobStatus{
		Status:       common.CHUNK_DONE,
		Artifact:     artifact,
		ArtifactType: artifact.GetType(),
		ArtifactPath: artifactNameAndStream.Name,
		StatusDttm:   time.Now(),
		IsChunked:    true,
		ChunkCount:   manifest.ChunkCount,
		TotalSize:    manifest.TotalSize,
		Chunks:       manifest.Chunks,
		Hash:         manifest.Hash,
	}

	// Обновляем статус в памяти
	jobStatusMap.SetJobStatus(jobId, successJobStatus)

	// Записываем метафайл с информацией о фрагментах
	err = WriteMeta(fsPath, jobId, successJobStatus)
	if err != nil {
		log.Printf("failed to write meta file: %v\n", err)
		jobStatusMap.SetJobStatus(jobId, common.JobStatus{
			Artifact:     artifact,
			Status:       common.META_WRITING_FAILED,
			ArtifactPath: artifactNameAndStream.Name,
			StatusDttm:   time.Now(),
		})
		return
	}

	// Очистка после доставки
	err = artifact.DeliverCleanup()
	if err != nil {
		log.Printf("failed cleanup for artifact %+v. Error: %v\n", artifact, err)
	}

	jobStatusMap.SetJobStatus(jobId, successJobStatus)
}

func calculateSHA256(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error opening file for hash calculation: %v", err)
		return ""
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		log.Printf("Error calculating hash: %v", err)
		return ""
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func GetJobStatus(c echo.Context) error {
	id := c.Param("jobId")
	return getJobStatusByJob(id, c)
}
func GetLatestJobStatus(c echo.Context) error {
	return getJobStatusByJob(latestJob, c)
}

func getJobStatusByJob(jobId string, c echo.Context) error {
	return c.JSONPretty(http.StatusOK, jobStatusMap.GetJobStatus(jobId), "  ")
}

type JobStatusMap struct {
	JobStatusMap map[string]common.JobStatus
	Lock         sync.RWMutex
}

func (jsm *JobStatusMap) GetJobStatus(jobId string) common.JobStatus {
	jsm.Lock.RLock()
	defer jsm.Lock.RUnlock()
	return jsm.JobStatusMap[jobId]
}

func (jsm *JobStatusMap) SetJobStatus(jobId string, jobStatus common.JobStatus) {
	jsm.Lock.Lock()
	defer jsm.Lock.Unlock()
	jsm.JobStatusMap[jobId] = jobStatus
}
func (jsm *JobStatusMap) checkDownloadingDoneJobs() {
	fsPath, err := getBaseFilePath(common.StartupConfig.NFSPath)
	if err != nil {
		log.Printf("failed to get path to store artifacts since %v", err)
		return
	}
	jsm.Lock.RLock()
	successJobs := make(map[string]common.JobStatus)
	for jobId, jobStatus := range jsm.JobStatusMap {
		if jobStatus.Status == common.DOWNLOADING_DONE {
			artifact := jobStatus.Artifact
			dstFilePath := filepath.Join(fsPath, common.GetJobMetaFileName(jobId))
			// if file exists just skip it
			if _, err := os.Stat(dstFilePath); errors.Is(err, os.ErrNotExist) {
				log.Printf("Job - %s: job is successfully finished", jobId)
				successJobs[jobId] = common.JobStatus{Artifact: artifact, Status: common.SUCCESS, StatusDttm: time.Now()}
			}
		}
	}
	jsm.Lock.RUnlock()

	for jobId, jobStatus := range successJobs {
		jsm.SetJobStatus(jobId, jobStatus)
	}
}

func (jsm *JobStatusMap) deleteStaleJobs() {
	jsm.Lock.Lock()
	for jobId, jobStatus := range jsm.JobStatusMap {
		if time.Since(jobStatus.StatusDttm) > 7*24*time.Hour {
			log.Printf("deleting job %s since it is stale", jobId)
			delete(jsm.JobStatusMap, jobId)
		}
	}
	jsm.Lock.Unlock()
}

func CheckDownloadingDoneJobs() {
	for {
		jobStatusMap.checkDownloadingDoneJobs()
		time.Sleep(15 * time.Second)
	}
}

func DeleteStaleJobs() {
	for {
		jobStatusMap.deleteStaleJobs()
		time.Sleep(1 * time.Hour)
	}
}

func WriteMeta(dirPath, jobId string, status common.JobStatus) error {
	metaFileName := filepath.Join(dirPath, common.GetJobMetaFileName(jobId))
	metaFile, err := os.Create(metaFileName)
	defer metaFile.Close()
	if err != nil {
		log.Println("failed to create meta file", metaFileName, err)
		return err
	}
	statusBytes, err := json.Marshal(status)
	if err != nil {
		log.Printf("failed to serialize jobStatus %+v with error %v\n", status, err)
		return err
	}

	if _, err := metaFile.Write(statusBytes); err != nil {
		log.Println("failed to write meta file", err)
		return err
	}
	return nil
}