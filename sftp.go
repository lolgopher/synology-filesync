package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func NewSFTPClient(ip, port, user, password string) (*sftp.Client, error) {
	// SSH 연결 정보 설정
	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// SSH 클라이언트 생성
	addr := fmt.Sprintf("%s:%s", ip, port)
	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %s", err)
	}

	// SFTP 클라이언트 생성
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create SFTP client: %s", err)
	}

	return sftpClient, nil
}

func SendFileOverSFTP(client *sftp.Client, localFilePath, remoteFilePath string) (int, error) {
	// 원격지에서 해당 파일이 이미 존재하는지 확인
	remoteFile, err := client.Stat(remoteFilePath)
	if err == nil {
		isSame, err := IsSameFileSize(localFilePath, remoteFile)
		if err != nil {
			return 0, fmt.Errorf("fail to check same file %s and %s: %v", localFilePath, remoteFilePath, err)
		}
		// 같은 파일인 경우
		if isSame {
			return 0, nil
		} else {
			return 0, fmt.Errorf("file %s already exist", remoteFilePath)
		}
	}

	// 파일 열기
	localFile, err := os.Open(localFilePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open local file: %s", err)
	}
	defer func() {
		if err := localFile.Close(); err != nil {
			log.Printf("fail to close %s file: %v", localFilePath, err)
		}
	}()

	localFileContent, err := io.ReadAll(localFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read local file: %s", err)
	}

	// 경로 생성
	dir := filepath.Dir(remoteFilePath)
	if _, err := client.Stat(dir); os.IsNotExist(err) {
		if err := client.MkdirAll(dir); err != nil {
			return 0, fmt.Errorf("failed to create remote dir: %s", err)
		}
	}

	// 파일 전송
	newFile, err := client.OpenFile(remoteFilePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL)
	if err != nil {
		return 0, fmt.Errorf("failed to create remote file: %s", err)
	}
	defer func() {
		if err := newFile.Close(); err != nil {
			log.Printf("fail to close %s file: %v", remoteFilePath, err)
		}
	}()

	size, err := newFile.Write(localFileContent)
	if err != nil {
		return 0, fmt.Errorf("failed to write to remote file: %s", err)
	}

	return size, nil
}
