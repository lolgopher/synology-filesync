package app

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/lolgopher/synology-filesync/protocol"
	"github.com/pkg/errors"
)

type UploadOptions struct {
	LocalPath        string
	SynologyPath     string
	ExcludePaths     []string
	SSHPath          string
	YAMLFilename     string
	SpareSpace       uint64
	UploadDelay      time.Duration
	UploadRetryDelay time.Duration
	UploadRetryCount int
}

type Uploader struct {
	options       UploadOptions
	metadataStore MetadataStore
	sftpFactory   SFTPFactory
	logger        Logger
	sleep         SleepFunc

	client   SFTPClient
	connInfo *protocol.ConnectionInfo
}

type InitialSFTPError struct {
	err error
}

type ReconnectSFTPError struct {
	err error
}

func (e *InitialSFTPError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}

	return e.err.Error()
}

func (e *InitialSFTPError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.err
}

func (e *InitialSFTPError) Cause() error {
	return e.Unwrap()
}

func (e *ReconnectSFTPError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}

	return e.err.Error()
}

func (e *ReconnectSFTPError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.err
}

func NewUploader(opts UploadOptions, metadataStore MetadataStore, sftpFactory SFTPFactory, logger Logger, sleep SleepFunc) *Uploader {
	if sleep == nil {
		sleep = time.Sleep
	}

	return &Uploader{
		options:       opts,
		metadataStore: metadataStore,
		sftpFactory:   sftpFactory,
		logger:        logger,
		sleep:         sleep,
	}
}

func (u *Uploader) Run(info *protocol.ConnectionInfo) error {
	client, err := u.sftpFactory(info)
	if err != nil {
		return &InitialSFTPError{err: err}
	}

	u.client = client
	u.connInfo = info
	defer func() {
		u.connInfo = nil
		if u.client != nil {
			if err := u.client.Close(); err != nil {
				u.logger.Printf("fail to close sftp client: %v", err)
			}
		}
		u.client = nil
	}()

	return u.Search(filepath.Join(u.options.LocalPath, u.options.SynologyPath))
}

func (u *Uploader) Search(folderPath string) error {
	err := filepath.Walk(folderPath, func(targetPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if len(u.options.ExcludePaths) > 0 {
			rel, err := filepath.Rel(u.options.LocalPath, targetPath)
			if err != nil {
				return err
			}

			candidate := path.Clean("/" + filepath.ToSlash(rel))
			if isExcludedSynologyPath(candidate, u.options.ExcludePaths) {
				if info.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}
		}

		if !info.IsDir() && info.Name() != "metadata.yaml" &&
			(u.options.YAMLFilename == "" || info.Name() != u.options.YAMLFilename) {
			targetMetadata, err := u.metadataStore.Read(filepath.Dir(targetPath), u.options.YAMLFilename)
			if err != nil {
				return err
			}

			metadata, ok := targetMetadata[targetPath]
			if !ok {
				return fmt.Errorf("fail to find %s in metadata", targetPath)
			}

			switch protocol.FileTransferStatus(metadata.Status) {
			case protocol.Init:
				u.logger.Printf("%s is init metadata status", targetPath)
				return nil
			case protocol.Sent:
				u.logger.Printf("%s has already been sent", targetPath)
				return nil
			case protocol.Failed:
				u.logger.Printf("%s sent failed", targetPath)
				return nil
			case protocol.NotSent:
				var result protocol.FileTransferStatus
				if size, err := u.Send(targetPath); err != nil {
					var reconnectErr *ReconnectSFTPError
					if errors.As(err, &reconnectErr) {
						return err
					}

					result = protocol.Failed
					u.logger.Printf("fail to %s not sent file: %v", targetPath, err)
				} else {
					result = protocol.Sent

					if size == 0 {
						u.logger.Printf("same size file %s already exist", targetPath)
					} else {
						u.logger.Printf("%s: %d", targetPath, size)
					}
				}

				if err := u.metadataStore.Write(targetPath, u.options.YAMLFilename, 0, result); err != nil {
					return err
				}
				u.sleep(u.options.UploadDelay)
			default:
				u.logger.Printf("%s is unknown status", metadata.Status)
				return nil
			}
		}

		return nil
	})

	return err
}

func (u *Uploader) Send(targetPath string) (int, error) {
	client := u.client
	var lastError error
	size := 0
	for i := 0; i < u.options.UploadRetryCount; i++ {
		destPath, _ := strings.CutPrefix(targetPath, u.options.LocalPath)
		destPath = filepath.Join(u.options.SSHPath, destPath)

		targetFileInfo, err := os.Stat(targetPath)
		if err != nil {
			lastError = fmt.Errorf("fail to get %s file info: %v", targetPath, err)
			u.logger.Print(lastError.Error())
		}

		freeSize, err := client.FreeSpace("/storage/emulated")
		freeSpaceErr := err
		if err != nil {
			lastError = errors.Wrap(err, "fail to get storage directory information")
			u.logger.Print(lastError.Error())
		}

		if targetFileInfo != nil && freeSpaceErr == nil {
			if targetSize := uint64(targetFileInfo.Size()); targetSize+u.options.SpareSpace > freeSize {
				lastError = fmt.Errorf("not enough space (\n"+
					"\ttarget file size: %d\n"+
					"\tfree space: %d\n"+
					"\tspare space: %d\n)", targetSize, freeSize, u.options.SpareSpace)
				u.logger.Printf(lastError.Error())
				u.logger.Printf("retrying...")
				u.sleep(u.options.UploadRetryDelay)
				continue
			}
		}

		size, err = client.SendFile(targetPath, destPath)
		if err != nil {
			lastError = fmt.Errorf("fail to %s send file over sftp: %v", targetPath, err)
			u.logger.Print(lastError.Error())

			errStr := errors.Cause(err).Error()
			if strings.Contains(errStr, "connection lost") ||
				strings.Contains(errStr, "no route to host") {
				newSFTP, err := u.sftpFactory(u.connInfo)
				if err != nil {
					return size, &ReconnectSFTPError{err: err}
				}

				_ = client.Close()
				client = newSFTP
				u.client = newSFTP
			}

			if err := client.RemoveFile(destPath); err != nil {
				u.logger.Printf("fail to remove %s remote file: %v", destPath, err)
			}
			u.logger.Printf("retrying...")
			u.sleep(u.options.UploadRetryDelay)
		} else {
			break
		}
	}

	return size, lastError
}
