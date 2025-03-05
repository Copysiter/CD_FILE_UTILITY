
package common

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

const (
	// Размер фрагмента по умолчанию - 50MB
	DefaultChunkSize int64 = 50 * 1024 * 1024
	ChunkPrefix             = "chunk_"
	ManifestSuffix            = ".manifest"
)

// FileChunk содержит информацию о фрагменте файла
type FileChunk struct {
	Index     int    `json:"index"`
	FileName  string `json:"fileName"`
	Size      int64  `json:"size"`
	TotalSize int64  `json:"totalSize"`
	IsLast    bool   `json:"isLast"`
}

// FileManifest содержит информацию о всем файле и его фрагментах
// Используется только внутри пакета, внешне данные хранятся в JobStatus
type FileManifest struct {
	OriginalFileName string         `json:"originalFileName"`
	TotalSize        int64          `json:"totalSize"`
	ChunkCount    int            `json:"chunkCount"`
	Chunks        []FileChunk `json:"chunks"`
	Completed        bool           `json:"completed"`
	MD5Hash          string         `json:"md5Hash,omitempty"`
	SHA256Hash       string         `json:"sha256Hash,omitempty"`
	Hash             string         `json:"hash,omitempty"`
}

// CalculateFileMD5 вычисляет MD5 хеш-сумму файла
func CalculateFileMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CalculateFileSHA256 вычисляет SHA256 хеш-сумму файла
func CalculateFileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// SplitFileIntoChunks разбивает файл на фрагменты
func SplitFileIntoChunks(filePath, targetDir string, chunkSize int64) (*FileManifest, error) {
	// Вычисляем хеш-суммы исходного файла перед разбиением
	md5Hash, err := CalculateFileMD5(filePath)
	if err != nil {
		log.Printf("Warning: Failed to calculate MD5 hash: %v", err)
		// Продолжаем без хеш-суммы, это не критическая ошибка
	}

	sha256Hash, err := CalculateFileSHA256(filePath)
	if err != nil {
		log.Printf("Warning: Failed to calculate SHA256 hash: %v", err)
		// Продолжаем без хеш-суммы, это не критическая ошибка
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %v", err)
	}

	totalSize := fileInfo.Size()
	chunkCount := (totalSize + chunkSize - 1) / chunkSize // округление вверх
	fileName := filepath.Base(filePath)

	manifest := &FileManifest{
		OriginalFileName: fileName,
		TotalSize:        totalSize,
		ChunkCount:    int(chunkCount),
		Chunks:        make([]FileChunk, 0, chunkCount),
		Completed:        false,
		MD5Hash:          md5Hash,
		SHA256Hash:       sha256Hash,
	}

	var offset int64 = 0
	for i := 0; i < int(chunkCount); i++ {
		currSize := chunkSize
		if offset+currSize > totalSize {
			currSize = totalSize - offset
		}

		chunkName := fmt.Sprintf("%s%d_%s", ChunkPrefix, i, fileName)
		chunkPath := filepath.Join(targetDir, chunkName)

		chunk := FileChunk{
			Index:     i,
			FileName:  chunkName,
			Size:      currSize,
			TotalSize: totalSize,
			IsLast:    i == int(chunkCount)-1,
		}

		manifest.Chunks = append(manifest.Chunks, chunk)

		chunkFile, err := os.Create(chunkPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create chunk file: %v", err)
		}

		_, err = file.Seek(offset, io.SeekStart)
		if err != nil {
			chunkFile.Close()
			return nil, fmt.Errorf("failed to seek in source file: %v", err)
		}

		_, err = io.CopyN(chunkFile, file, currSize)
		chunkFile.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to write chunk: %v", err)
		}

		offset += currSize
	}

	// Сохраняем манифест в файл
	manifestPath := filepath.Join(targetDir, fileName+ManifestSuffix)
	manifestFile, err := os.Create(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest file: %v", err)
	}
	defer manifestFile.Close()

	encoder := json.NewEncoder(manifestFile)
	err = encoder.Encode(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to write manifest: %v", err)
	}

	return manifest, nil
}

// MergeChunks собирает фрагменты в один файл
func MergeChunks(manifestPath, targetDir string) (string, error) {
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return "", fmt.Errorf("failed to open manifest: %v", err)
	}
	defer manifestFile.Close()

	var manifest FileManifest
	decoder := json.NewDecoder(manifestFile)
	err = decoder.Decode(&manifest)
	if err != nil {
		return "", fmt.Errorf("failed to parse manifest: %v", err)
	}

	outputPath := filepath.Join(targetDir, manifest.OriginalFileName)
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %v", err)
	}
	defer outputFile.Close()

	manifestDir := filepath.Dir(manifestPath)

	// Проверяем наличие всех фрагментов
	for i, chunk := range manifest.Chunks {
		chunkPath := filepath.Join(manifestDir, chunk.FileName)
		if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
			return "", fmt.Errorf("chunk %d is missing: %s", i, chunkPath)
		}
	}

	// Собираем файл из фрагментов
	for i := 0; i < manifest.ChunkCount; i++ {
		chunkName := fmt.Sprintf("%s%d_%s", ChunkPrefix, i, manifest.OriginalFileName)
		chunkPath := filepath.Join(manifestDir, chunkName)
		
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			return "", fmt.Errorf("failed to open chunk %d: %v", i, err)
		}

		_, err = io.Copy(outputFile, chunkFile)
		chunkFile.Close()
		if err != nil {
			return "", fmt.Errorf("failed to write chunk %d to output: %v", i, err)
		}
	}
	
	// Закрываем файл, чтобы можно было проверить хеш-сумму
	outputFile.Close()
	
	// Проверяем хеш-суммы, если они были заданы в манифесте
	if manifest.MD5Hash != "" {
		calculatedMD5, err := CalculateFileMD5(outputPath)
		if err != nil {
			log.Printf("Warning: Failed to calculate MD5 hash for merged file: %v", err)
		} else if calculatedMD5 != manifest.MD5Hash {
			return "", fmt.Errorf("MD5 hash mismatch for merged file: expected %s, got %s", 
				manifest.MD5Hash, calculatedMD5)
		} else {
			log.Printf("MD5 hash verification successful for %s", outputPath)
		}
	}
	
	if manifest.SHA256Hash != "" {
		calculatedSHA256, err := CalculateFileSHA256(outputPath)
		if err != nil {
			log.Printf("Warning: Failed to calculate SHA256 hash for merged file: %v", err)
		} else if calculatedSHA256 != manifest.SHA256Hash {
			return "", fmt.Errorf("SHA256 hash mismatch for merged file: expected %s, got %s", 
				manifest.SHA256Hash, calculatedSHA256)
		} else {
			log.Printf("SHA256 hash verification successful for %s", outputPath)
		}
	}

	// Обновляем манифест как завершенный
	manifest.Completed = true
	updatedManifestFile, err := os.Create(manifestPath)
	if err != nil {
		log.Printf("Warning: failed to update manifest status: %v", err)
	} else {
		encoder := json.NewEncoder(updatedManifestFile)
		err = encoder.Encode(manifest)
		updatedManifestFile.Close()
		if err != nil {
			log.Printf("Warning: failed to write updated manifest: %v", err)
		}
	}

	return outputPath, nil
}

// CheckChunkSize проверяет и возвращает размер фрагмента
func CheckChunkSize(configSize string) int64 {
	if configSize == "" {
		return DefaultChunkSize
	}

	// Попытка парсинга ChunkSize из конфига
	var multiplier int64 = 1
	var sizeValue string

	if len(configSize) > 2 {
		unit := configSize[len(configSize)-2:]
		if unit == "MB" {
			multiplier = 1024 * 1024
			sizeValue = configSize[:len(configSize)-2]
		} else if unit == "KB" {
			multiplier = 1024
			sizeValue = configSize[:len(configSize)-2]
		} else {
			sizeValue = configSize
		}
	} else {
		sizeValue = configSize
	}

	size, err := strconv.ParseInt(sizeValue, 10, 64)
	if err != nil {
		log.Printf("Failed to parse chunk size '%s', using default: %v", configSize, err)
		return DefaultChunkSize
	}

	return size * multiplier
}

// IsManifestFile проверяет, является ли файл манифестом
func IsManifestFile(fileName string) bool {
	return len(fileName) > len(ManifestSuffix) && 
		fileName[len(fileName)-len(ManifestSuffix):] == ManifestSuffix
}

// GetManifestFromChunkName извлекает имя манифеста из имени фрагмента
func GetManifestFromChunkName(chunkName string) string {
	if len(chunkName) <= len(ChunkPrefix) {
		return ""
	}
	
	if chunkName[:len(ChunkPrefix)] != ChunkPrefix {
		return ""
	}
	
	// Ищем первое подчеркивание после префикса
	underscorePos := -1
	for i := len(ChunkPrefix); i < len(chunkName); i++ {
		if chunkName[i] == '_' {
			underscorePos = i
			break
		}
	}
	
	if underscorePos == -1 {
		return ""
	}
	
	// Возвращаем имя оригинального файла после подчеркивания
	return chunkName[underscorePos+1:] + ManifestSuffix
}
