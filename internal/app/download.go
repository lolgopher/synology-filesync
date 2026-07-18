package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lolgopher/synology-filesync/protocol"
	"golang.org/x/sync/semaphore"
)

type WorkerErrorHandler func(error)

type DownloadOptions struct {
	RootRemotePath   string
	LocalPath        string
	MetadataFilename string
	WorkerLimit      int64
	ExcludePaths     []string

	MkdirAll func(string, os.FileMode) error
	Remove   func(string) error

	SynologyFactory    SynologyFactory
	MetadataStore      MetadataStore
	Logger             Logger
	WorkerErrorHandler WorkerErrorHandler
}

type Downloader struct {
	rootRemotePath     string
	localPath          string
	metadataFilename   string
	workerLimit        int64
	excludePaths       []string
	mkdirAll           func(string, os.FileMode) error
	remove             func(string) error
	synologyFactory    SynologyFactory
	metadataStore      MetadataStore
	logger             Logger
	workerErrorHandler WorkerErrorHandler
}

func NewDownloader(opts DownloadOptions) *Downloader {
	mkdirAll := opts.MkdirAll
	if mkdirAll == nil {
		mkdirAll = os.MkdirAll
	}

	remove := opts.Remove
	if remove == nil {
		remove = os.Remove
	}

	return &Downloader{
		rootRemotePath:     opts.RootRemotePath,
		localPath:          opts.LocalPath,
		metadataFilename:   opts.MetadataFilename,
		workerLimit:        opts.WorkerLimit,
		excludePaths:       opts.ExcludePaths,
		mkdirAll:           mkdirAll,
		remove:             remove,
		synologyFactory:    opts.SynologyFactory,
		metadataStore:      opts.MetadataStore,
		logger:             opts.Logger,
		workerErrorHandler: opts.WorkerErrorHandler,
	}
}

func (d *Downloader) Run(info *protocol.ConnectionInfo) error {
	client, err := d.synologyFactory(info)
	if err != nil {
		return fmt.Errorf("fail to make synology client: %v", err)
	}

	workerErr := &downloadWorkerFirstError{}
	sem := semaphore.NewWeighted(d.workerLimit)
	wg := &sync.WaitGroup{}

	fileListResp, err := d.searchSynologyRecursive(client, d.rootRemotePath, 0)
	if err != nil {
		return fmt.Errorf("fail to search from synology filestation: %v", err)
	}

	if err := d.downloadSynologyRecursive(client, fileListResp, sem, wg, workerErr); err != nil {
		return fmt.Errorf("fail to download from synology filestation: %v", err)
	}

	wg.Wait()
	if err := workerErr.Err(); err != nil {
		return err
	}

	d.logger.Print("Done!")
	return nil
}

func (d *Downloader) searchSynologyRecursive(client SynologyClient, folderPath string, depth int) (*protocol.FileListResponse, error) {
	if isExcludedSynologyPath(folderPath, d.excludePaths) {
		return &protocol.FileListResponse{Success: true}, nil
	}

	fileListResp, err := client.GetFileList(folderPath)
	if err != nil {
		return nil, err
	}

	for _, file := range fileListResp.Data.Files {
		if isExcludedSynologyPath(file.Path, d.excludePaths) {
			continue
		}

		if file.IsDir {
			if !isRecycleDirectory(file.Name) {
				if err := d.mkdirAll(filepath.Join(d.localPath, file.Path), os.ModePerm); err != nil {
					return nil, fmt.Errorf("fail to make download folder: %v", err)
				}

				file.List, err = d.searchSynologyRecursive(client, file.Path, depth+1)
				if err != nil {
					return nil, err
				}
			}
			continue
		}

		initFilePath := filepath.Join(d.localPath, file.Path)
		if err := d.initializeMetadata(initFilePath, file.Additional.Size); err != nil {
			return nil, err
		}
	}

	return fileListResp, nil
}

func (d *Downloader) downloadSynologyRecursive(client SynologyClient, fileList *protocol.FileListResponse, sem *semaphore.Weighted, wg *sync.WaitGroup, workerErr *downloadWorkerFirstError) error {
	ctx := context.Background()

	for _, file := range fileList.Data.Files {
		if isExcludedSynologyPath(file.Path, d.excludePaths) {
			continue
		}

		if file.IsDir {
			if !isRecycleDirectory(file.Name) {
				if err := d.downloadSynologyRecursive(client, file.List, sem, wg, workerErr); err != nil {
					return err
				}
			}
			continue
		}

		for {
			if err := sem.Acquire(ctx, 1); err != nil {
				d.logger.Printf("fail to acquire semaphore: %v", err)
				continue
			}
			break
		}

		filePath := file.Path
		wg.Add(1)
		go func() {
			defer func() {
				sem.Release(1)
				wg.Done()
			}()

			if err := d.downloadFile(client, filePath); err != nil {
				if d.workerErrorHandler != nil {
					d.workerErrorHandler(err)
				}
				workerErr.Add(err)
			}
		}()
	}

	return nil
}

func (d *Downloader) downloadFile(client SynologyClient, filePath string) error {
	targetPath := filepath.Join(d.localPath, filePath)

	targetMetadata, err := d.metadataStore.Read(filepath.Dir(targetPath), d.metadataFilename)
	if err != nil {
		return err
	}

	if metadata, ok := targetMetadata[targetPath]; ok && protocol.FileTransferStatus(metadata.Status) != protocol.Init {
		d.logger.Printf("%s has already been downloaded", targetPath)
		return nil
	}

	downloadFilePath, _, err := client.DownloadFile(filePath, targetPath)
	if err != nil {
		return fmt.Errorf("fail to %s download file: %v", filePath, err)
	}

	if err := d.metadataStore.Write(downloadFilePath, d.metadataFilename, 0, protocol.NotSent); err != nil {
		return fmt.Errorf("fail to %s write metadata: %v", downloadFilePath, err)
	}

	d.logger.Printf("%s success download", targetPath)
	return nil
}

func (d *Downloader) initializeMetadata(filePath string, size uint64) error {
	metadataFilePath := filepath.Join(filepath.Dir(filePath), d.metadataFilename)
	if !d.metadataStore.Exists(metadataFilePath) {
		if err := d.metadataStore.Write(filePath, d.metadataFilename, size, protocol.Init); err != nil {
			return fmt.Errorf("fail to %s write metadata: %v", filePath, err)
		}
		d.logger.Printf("init %s metadata", filePath)
		return nil
	}

	targetMetadata, err := d.metadataStore.Read(filepath.Dir(filePath), d.metadataFilename)
	if err != nil {
		return err
	}

	if metadata, ok := targetMetadata[filePath]; !ok || metadata.Size != size {
		if err := d.metadataStore.Write(filePath, d.metadataFilename, size, protocol.Init); err != nil {
			return fmt.Errorf("fail to %s write metadata: %v", filePath, err)
		}
		d.logger.Printf("init %s metadata", filePath)

		if d.metadataStore.Exists(filePath) {
			if err := d.remove(filePath); err != nil {
				return fmt.Errorf("fail to %s remove file: %v", filePath, err)
			}
			d.logger.Printf("remove %s file", filePath)
		}
		return nil
	}

	d.logger.Printf("%s metadata already exists", filePath)
	return nil
}

func isRecycleDirectory(name string) bool {
	return name == "#recycle"
}

type downloadWorkerFirstError struct {
	mu  sync.Mutex
	err error
}

func (e *downloadWorkerFirstError) Add(err error) {
	if err == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err == nil {
		e.err = err
	}
}

func (e *downloadWorkerFirstError) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.err
}
