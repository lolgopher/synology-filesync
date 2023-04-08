package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileListResponse struct {
	Data struct {
		Files []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"isdir"`
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
	resp, err := http.Get(fmt.Sprintf("http://%s:%s/webapi/auth.cgi?%s", ip, port, apiInfo.Encode()))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// API 응답 해석
	var authResponse struct {
		Data struct {
			Sid string `json:"sid"`
		} `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&authResponse)
	if err != nil {
		return "", err
	}

	if authResponse.Data.Sid == "" {
		// 인증 실패 처리
		var errorResponse ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errorResponse)
		if err != nil {
			return "", err
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

	resp, err := http.Get(fmt.Sprintf("http://%s:%s/webapi/entry.cgi?%s", ip, port, listInfo.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// API 응답 해석
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	fileListResponse := &FileListResponse{}
	err = json.Unmarshal(body, fileListResponse)
	if err != nil {
		return nil, err
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

	resp, err := http.Get(fmt.Sprintf("http://%s:%s/webapi/entry.cgi?%s", ip, port, downloadInfo.Encode()))
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	// 파일 다운로드
	tempPath := destPath + ".download"
	out, err := os.Create(tempPath)
	if err != nil {
		return "", 0, err
	}
	defer out.Close()

	size, err := io.Copy(out, resp.Body)
	if err != nil {
		return "", 0, err
	}
	out.Close()

	// 같은 파일인지 확인
	existFile, err := os.Stat(destPath)
	if err == nil {
		isSame, err := isSameFileSize(tempPath, existFile)
		if err != nil {
			return "", 0, err
		}
		if !isSame {
			destPath = getUniqueFilePath(destPath)
		}
	}

	errCnt := 0
	for {
		if err := os.Rename(tempPath, destPath); err != nil {
			if !fileExists(tempPath) {
				break
			}

			errCnt += 1
			if errCnt >= 10 {
				return "", 0, err
			}
		} else {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return destPath, size, nil
}

func isSameFileSize(targetFile string, compareFile fs.FileInfo) (bool, error) {
	target, err := os.Stat(targetFile)
	if err != nil {
		return false, err
	}

	if target.Size() != compareFile.Size() {
		return false, nil
	}

	return true, nil
}

func getUniqueFilePath(filePath string) string {
	dir := filepath.Dir(filePath)
	ext := filepath.Ext(filePath)
	name := strings.TrimSuffix(filepath.Base(filePath), ext)

	// 파일 이름 뒤에 1부터 차례대로 숫자를 붙여가며 존재하지 않는 파일 이름 찾기
	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s_%d%s", name, i, ext)
		if !fileExists(filepath.Join(dir, newName)) {
			return filepath.Join(dir, newName)
		}
	}
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
