package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pkg/errors"
)

type SynologyClient struct {
	ConnInfo *ConnectionInfo
	SessID   string
}

type FileListResponse struct {
	Data struct {
		Files  []*File `json:"files"`
		Offset int     `json:"offset"`
		Total  int     `json:"total"`
	} `json:"data"`
	Success bool `json:"success"`
}

type File struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	IsDir      bool   `json:"isdir"`
	Additional struct {
		Size uint64 `json:"size"`
	} `json:"additional"`
	List *FileListResponse
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewSynologyClient(info *ConnectionInfo) (*SynologyClient, error) {
	sid, err := newSessionID(info)
	if err != nil {
		return nil, errors.Wrap(err, "fail to get new session id")
	}

	return &SynologyClient{
		ConnInfo: info,
		SessID:   sid,
	}, nil
}

func newSessionID(info *ConnectionInfo) (string, error) {
	// File Station API 인증 정보
	apiInfo := url.Values{}
	apiInfo.Set("api", "SYNO.API.Auth")
	apiInfo.Set("version", "6")
	apiInfo.Set("method", "login")
	apiInfo.Set("account", info.Username)
	apiInfo.Set("passwd", info.Password)
	apiInfo.Set("session", "FileStation")

	// 인증 API 호출
	synoURL := fmt.Sprintf("http://%s:%s/webapi/auth.cgi?%s", info.IP, info.Port, apiInfo.Encode())
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

func (client *SynologyClient) GetFileList(folderPath string) (*FileListResponse, error) {
	// FileStation.List API 호출
	listInfo := url.Values{}
	listInfo.Set("api", "SYNO.FileStation.List")
	listInfo.Set("version", "1")
	listInfo.Set("method", "list")
	listInfo.Set("folder_path", folderPath)
	listInfo.Set("_sid", client.SessID)
	listInfo.Set("additional", "size")

	synoURL := fmt.Sprintf("http://%s:%s/webapi/entry.cgi?%s", client.ConnInfo.IP, client.ConnInfo.Port, listInfo.Encode())
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
		log.Printf("error to unmarshal body data: %s", string(body))
		return nil, fmt.Errorf("fail to unmarshal %s response body: %v", synoURL, err)
	}

	if !fileListResponse.Success {
		log.Printf("success flag is false: %v", fileListResponse)
	}
	return fileListResponse, nil
}

func (client *SynologyClient) DownloadFile(filePath, destPath string) (string, int64, error) {
	// FileStation.Download API 호출
	downloadInfo := url.Values{}
	downloadInfo.Set("api", "SYNO.FileStation.Download")
	downloadInfo.Set("version", "1")
	downloadInfo.Set("method", "download")
	downloadInfo.Set("path", filePath)
	downloadInfo.Set("_sid", client.SessID)

	synoURL := fmt.Sprintf("http://%s:%s/webapi/entry.cgi?%s", client.ConnInfo.IP, client.ConnInfo.Port, downloadInfo.Encode())
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

	// 방어 코드
	if !FileExists(tempPath) {
		if !FileExists(destPath) {
			return "", 0, fmt.Errorf("file missing after download %s file", tempPath)
		} else {
			return destPath, size, nil
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
