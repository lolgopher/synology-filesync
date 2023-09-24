package protocol

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SFTPClient struct {
	ConnInfo *ConnectionInfo
	Client   *sftp.Client
}

func NewSFTPClient(info *ConnectionInfo) (*SFTPClient, error) {
	// SSH 연결 정보 설정
	sshConfig := &ssh.ClientConfig{
		User: info.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(info.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// SSH 클라이언트 생성
	addr := fmt.Sprintf("%s:%d", info.IP, info.Port)
	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, errors.Wrap(err, "fail to dial")
	}

	// SFTP 클라이언트 생성
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, errors.Wrap(err, "fail to create SFTP client")
	}

	return &SFTPClient{
		ConnInfo: info,
		Client:   sftpClient,
	}, nil
}

func (sc *SFTPClient) SendFile(localFilePath, remoteFilePath string) (int, error) {
	// 원격지에서 해당 파일이 이미 존재하는지 확인
	remoteFile, err := sc.Client.Stat(remoteFilePath)
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
		return 0, errors.Wrap(err, "fail to open local file")
	}
	defer func() {
		if err := localFile.Close(); err != nil {
			log.Printf("fail to close %s file: %v", localFilePath, err)
		}
	}()

	localFileContent, err := io.ReadAll(localFile)
	if err != nil {
		return 0, errors.Wrap(err, "fail to read local file")
	}

	// 경로 생성
	dir := filepath.Dir(remoteFilePath)
	if _, err := sc.Client.Stat(dir); os.IsNotExist(err) {
		if err := sc.Client.MkdirAll(dir); err != nil {
			return 0, errors.Wrap(err, "fail to create remote dir")
		}
	}

	// 파일 전송
	newFile, err := sc.Client.OpenFile(remoteFilePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL)
	if err != nil {
		return 0, errors.Wrap(err, "fail to create remote file")
	}
	defer func() {
		if err := newFile.Close(); err != nil {
			log.Printf("fail to close %s file: %v", remoteFilePath, err)
		}
	}()

	size, err := newFile.Write(localFileContent)
	if err != nil {
		return 0, errors.Wrap(err, "fail to write to remote file")
	}

	return size, nil
}

func (sc *SFTPClient) RemoveFile(targetFilePath string) error {
	return sc.Client.Remove(targetFilePath)
}

func (sc *SFTPClient) Close() error {
	return sc.Client.Close()
}
