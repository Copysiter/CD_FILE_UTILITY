
package deploy

import (
	"encoding/json"
	"fmt"
	"fts-cd-file-utility/common"
	"github.com/hirochachacha/go-smb2"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadChunkedFile загружает фрагментированный файл из SMB
func LoadChunkedFile(fs *smb2.Share, manifestPath, jobFilePath string) (string, error) {
	// Проверяем доступ к манифесту
	manifestFile, err := fs.Open(manifestPath)
	if err != nil {
		log.Printf("Error opening manifest file: %v\n", err)
		return "", fmt.Errorf("failed to open manifest: %v", err)
	}
	defer manifestFile.Close()

	// Читаем манифест
	var manifest common.FileManifest
	decoder := json.NewDecoder(manifestFile)
	err = decoder.Decode(&manifest)
	if err != nil {
		log.Printf("Error parsing manifest: %v\n", err)
		return "", fmt.Errorf("failed to parse manifest: %v", err)
	}

	// Создаем временную директорию для фрагментов
	tempDir := os.TempDir()
	chunkDir := filepath.Join(tempDir, "chunks_"+time.Now().Format("20060102150405"))
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		log.Printf("Error creating chunk directory: %v\n", err)
		return "", fmt.Errorf("failed to create chunk directory: %v", err)
	}

	// Получаем путь к директории манифеста
	manifestParentDir := filepath.Dir(manifestPath)

	// Загружаем каждый фрагмент
	for _, chunk := range manifest.Chunks {
		chunkPath := filepath.Join(manifestParentDir, chunk.FileName)
		localChunkPath := filepath.Join(chunkDir, chunk.FileName)

		log.Printf("Loading chunk %d/%d: %s\n", chunk.Index+1, manifest.ChunkCount, chunk.FileName)

		// Открываем фрагмент на SMB
		chunkFile, err := fs.Open(chunkPath)
		if err != nil {
			log.Printf("Error opening chunk file: %v\n", err)
			return "", fmt.Errorf("failed to open chunk %d: %v", chunk.Index, err)
		}

		// Создаем локальный файл фрагмента
		localChunk, err := os.Create(localChunkPath)
		if err != nil {
			chunkFile.Close()
			log.Printf("Error creating local chunk file: %v\n", err)
			return "", fmt.Errorf("failed to create local chunk %d: %v", chunk.Index, err)
		}

		// Копируем содержимое
		_, err = io.Copy(localChunk, chunkFile)
		chunkFile.Close()
		localChunk.Close()

		if err != nil {
			log.Printf("Error copying chunk content: %v\n", err)
			return "", fmt.Errorf("failed to copy chunk %d: %v", chunk.Index, err)
		}
	}

	// Сохраняем манифест локально
	localManifestPath := filepath.Join(chunkDir, manifest.OriginalFileName+common.ManifestSuffix)
	localManifest, err := os.Create(localManifestPath)
	if err != nil {
		log.Printf("Error creating local manifest: %v\n", err)
		return "", fmt.Errorf("failed to create local manifest: %v", err)
	}
	encoder := json.NewEncoder(localManifest)
	err = encoder.Encode(manifest)
	localManifest.Close()
	if err != nil {
		log.Printf("Error writing local manifest: %v\n", err)
		return "", fmt.Errorf("failed to write local manifest: %v", err)
	}

	// Собираем файл из фрагментов
	log.Printf("Merging chunks for %s\n", manifest.OriginalFileName)
	outputPath, err := common.MergeChunks(localManifestPath, tempDir)
	if err != nil {
		log.Printf("Error merging chunks: %v\n", err)
		return "", fmt.Errorf("failed to merge chunks: %v", err)
	}
	
	// Дополнительная проверка хеш-сумм (на случай, если MergeChunks не выполнил проверку)
	if manifest.MD5Hash != "" || manifest.SHA256Hash != "" {
		log.Printf("Verifying hash sums for merged file: %s\n", outputPath)
		
		if manifest.MD5Hash != "" {
			calculatedMD5, err := common.CalculateFileMD5(outputPath)
			if err != nil {
				log.Printf("Warning: Failed to calculate MD5 hash: %v\n", err)
			} else if calculatedMD5 != manifest.MD5Hash {
				return "", fmt.Errorf("MD5 hash verification failed: expected %s, got %s", 
					manifest.MD5Hash, calculatedMD5)
			} else {
				log.Printf("MD5 hash verified successfully: %s\n", calculatedMD5)
			}
		}
		
		if manifest.SHA256Hash != "" {
			calculatedSHA256, err := common.CalculateFileSHA256(outputPath)
			if err != nil {
				log.Printf("Warning: Failed to calculate SHA256 hash: %v\n", err)
			} else if calculatedSHA256 != manifest.SHA256Hash {
				return "", fmt.Errorf("SHA256 hash verification failed: expected %s, got %s", 
					manifest.SHA256Hash, calculatedSHA256)
			} else {
				log.Printf("SHA256 hash verified successfully: %s\n", calculatedSHA256)
			}
		}
	}

	// Очищаем временные фрагменты
	if err := os.RemoveAll(chunkDir); err != nil {
		log.Printf("Warning: failed to clean up chunk directory: %v\n", err)
	}

	// Удаляем файлы фрагментов и манифест на SMB после успешной сборки
	for _, chunk := range manifest.Chunks {
		chunkPath := filepath.Join(manifestParentDir, chunk.FileName)
		if err := fs.Remove(chunkPath); err != nil {
			log.Printf("Warning: failed to remove chunk %s: %v\n", chunkPath, err)
		}
	}
	if err := fs.Remove(manifestPath); err != nil {
		log.Printf("Warning: failed to remove manifest %s: %v\n", manifestPath, err)
	}
	if err := fs.Remove(jobFilePath); err != nil {
		log.Printf("Warning: failed to remove job file %s: %v\n", jobFilePath, err)
	}

	return outputPath, nil
}

// IsChunkedArtifact проверяет, является ли файл фрагментированным артефактом
func IsChunkedArtifact(jobFileContent []byte) bool {
	var jobStatus common.JobStatus
	err := json.Unmarshal(jobFileContent, &jobStatus)
	if err != nil {
		return false
	}
	
	return jobStatus.Status == common.CHUNK_DONE
}

// IsManifestFile проверяет, является ли файл манифестом фрагментов
func IsManifestFile(fileName string) bool {
	return common.IsManifestFile(fileName)
}

// TryProcessChunkedArtifact проверяет наличие фрагментированного артефакта
func TryProcessChunkedArtifact(fs *smb2.Share, jobFileContent []byte, jobFilePath string) (bool, string, error) {
	var jobStatus common.JobStatus
	err := json.Unmarshal(jobFileContent, &jobStatus)
	if err != nil {
		return false, "", fmt.Errorf("failed to parse job status: %v", err)
	}
	
	// Проверяем, является ли артефакт фрагментированным
	if !jobStatus.IsChunked {
		return false, "", nil
	}
	
	// Получаем путь к директории с фрагментами
	jobDir := filepath.Dir(jobFilePath)
	
	// Создаем временный манифест из данных в .job файле
	tempDir := os.TempDir()
	tempManifestPath := filepath.Join(tempDir, jobStatus.ArtifactPath+common.ManifestSuffix)
	
	manifest := common.FileManifest{
		OriginalFileName: jobStatus.ArtifactPath,
		TotalSize:        jobStatus.TotalSize,
		ChunkCount:       jobStatus.ChunkCount,
		Chunks:           jobStatus.Chunks,
		Completed:        false,
		MD5Hash:          jobStatus.MD5Hash,
		SHA256Hash:       jobStatus.SHA256Hash,
	}
	
	// Записываем временный манифест
	manifestFile, err := os.Create(tempManifestPath)
	if err != nil {
		return false, "", fmt.Errorf("failed to create temp manifest: %v", err)
	}
	
	encoder := json.NewEncoder(manifestFile)
	err = encoder.Encode(manifest)
	manifestFile.Close()
	if err != nil {
		return false, "", fmt.Errorf("failed to write temp manifest: %v", err)
	}

	// Определяем путь к манифесту
	manifestPath := filepath.Join(jobDir, jobStatus.ArtifactPath+common.ManifestSuffix)
	
	// Проверяем существование манифеста
	_, err = fs.Stat(manifestPath)
	if err != nil {
		// Если манифест не найден, проверяем поддиректорию chunks_<jobId>
		jobId := strings.TrimSuffix(filepath.Base(jobFilePath), ".job")
		chunkDir := filepath.Join(jobDir, "chunks_"+jobId)
		manifestPath = filepath.Join(chunkDir, jobStatus.ArtifactPath+common.ManifestSuffix)
		
		_, err = fs.Stat(manifestPath)
		if err != nil {
			return false, "", fmt.Errorf("manifest not found at %s: %v", manifestPath, err)
		}
	}

	// Собираем файл из фрагментов
	mergedFilePath, err := LoadChunkedFile(fs, manifestPath, jobFilePath)
	if err != nil {
		return true, "", fmt.Errorf("failed to load chunked file: %v", err)
	}

	return true, mergedFilePath, nil
}
