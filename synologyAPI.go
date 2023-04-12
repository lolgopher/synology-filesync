package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

type FileListResponse struct {
	Data struct {
		Files []struct {
			Name       string `json:"name"`
			Path       string `json:"path"`
			IsDir      bool   `json:"isdir"`
			Additional struct {
				Size int `json:"size"`
			} `json:"additional"`
			List *FileListResponse
		} `json:"files"`
		Offset int `json:"offset"`
		Total  int `json:"total"`
	} `json:"data"`
	Success bool `json:"success"`
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func GetSessionID(ip, port, username, password string) (string, error) {
	// File Station API 인증 정보
	apiInfo := url.Values{}
	apiInfo.Set("api", "SYNO.API.Auth")
	apiInfo.Set("version", "6")
	apiInfo.Set("method", "login")
	apiInfo.Set("account", username)
	apiInfo.Set("passwd", password)
	apiInfo.Set("session", "FileStation")

	// 인증 API 호출
	synoURL := fmt.Sprintf("http://%s:%s/webapi/auth.cgi?%s", ip, port, apiInfo.Encode())
	resp, err := http.Get(synoURL)
	if err != nil {
		return "", fmt.Errorf("fail to get %s url: %v", synoURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("fail to close %s request: %v", synoURL, err)
		}
	}()

	// API 응답 해석
	var authResponse struct {
		Data struct {
			Sid string `json:"sid"`
		} `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&authResponse)
	if err != nil {
		return "", fmt.Errorf("fail to decode %s response body: %v", synoURL, err)
	}

	if authResponse.Data.Sid == "" {
		// 인증 실패 처리
		var errorResponse ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errorResponse)
		if err != nil {
			return "", fmt.Errorf("fail to decode %s authentication failed response body: %v", synoURL, err)
		}
		return "", fmt.Errorf("authentication failed: %v", errorResponse)
	}

	return authResponse.Data.Sid, nil
}

func GetFileList(ip, port, sid, folderPath string) (*FileListResponse, error) {
	// FileStation.List API 호출
	listInfo := url.Values{}
	listInfo.Set("api", "SYNO.FileStation.List")
	listInfo.Set("version", "1")
	listInfo.Set("method", "list")
	listInfo.Set("folder_path", folderPath)
	listInfo.Set("_sid", sid)
	listInfo.Set("additional", "size")

	synoURL := fmt.Sprintf("http://%s:%s/webapi/entry.cgi?%s", ip, port, listInfo.Encode())
	resp, err := http.Get(synoURL)
	if err != nil {
		return nil, fmt.Errorf("fail to get %s url: %v", synoURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("fail to close %s request: %v", synoURL, err)
		}
	}()

	// API 응답 해석
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fail to read %s response body: %v", synoURL, err)
	}

	fileListResponse := &FileListResponse{}
	err = json.Unmarshal(body, fileListResponse)
	if err != nil {
		return nil, fmt.Errorf("fail to unmarshal %s response body: %v", synoURL, err)
	}

	return fileListResponse, nil
}

func DownloadFile(ip, port, sid, filePath, destPath string) (string, int64, error) {
	// FileStation.Download API 호출
	downloadInfo := url.Values{}
	downloadInfo.Set("api", "SYNO.FileStation.Download")
	downloadInfo.Set("version", "1")
	downloadInfo.Set("method", "download")
	downloadInfo.Set("path", filePath)
	downloadInfo.Set("_sid", sid)

	synoURL := fmt.Sprintf("http://%s:%s/webapi/entry.cgi?%s", ip, port, downloadInfo.Encode())
	resp, err := http.Get(synoURL)
	if err != nil {
		return "", 0, fmt.Errorf("fail to get %s url: %v", synoURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("fail to close %s request: %v", synoURL, err)
		}
	}()

	// 파일 다운로드
	tempPath := destPath + ".download"
	out, err := os.Create(tempPath)
	if err != nil {
		return "", 0, fmt.Errorf("fail to create %s file: %v", tempPath, err)
	}
	defer func() {
		if err := out.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("fail to close %s file: %v", tempPath, err)
		}
	}()

	size, err := io.Copy(out, resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("fail to copy %s file: %v", tempPath, err)
	}
	if err := out.Close(); err != nil {
		log.Printf("fail to close %s file: %v", tempPath, err)
	}

	if !FileExists(tempPath) {
		if !FileExists(destPath) {
			return "", 0, fmt.Errorf("file missing after download %s file", tempPath)
		} else {
			return destPath, size, nil
		}
	}

	// 같은 파일인지 확인
	existFile, err := os.Stat(destPath)
	if err == nil {
		isSame, err := IsSameFileSize(tempPath, existFile)
		if err != nil {
			return "", 0, fmt.Errorf("fail to check same file %s and %s: %v", destPath, tempPath, err)
		}
		if !isSame {
			destPath = GetUniqueFilePath(destPath)
		}
	}

	errCnt := 0
	for {
		if err := os.Rename(tempPath, destPath); err != nil {
			if !FileExists(tempPath) {
				break
			}

			errCnt += 1
			if errCnt >= 10 {
				return "", 0, fmt.Errorf("fail to rename filename %s to %s: %v", tempPath, destPath, err)
			}
		} else {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return destPath, size, nil
}
