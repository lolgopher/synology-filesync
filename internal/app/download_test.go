package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lolgopher/synology-filesync/protocol"
	"golang.org/x/sync/semaphore"
)

func TestIsRecycleDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "exact match", input: "#recycle", want: true},
		{name: "different case", input: "#Recycle", want: false},
		{name: "prefix only", input: "#recycle-bin", want: false},
		{name: "suffix only", input: "bin-#recycle", want: false},
		{name: "empty", input: "", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isRecycleDirectory(tt.input); got != tt.want {
				t.Fatalf("isRecycleDirectory(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDownloaderRun_FactoryError(t *testing.T) {
	t.Parallel()

	logger := &testLogger{}
	d := NewDownloader(DownloadOptions{
		WorkerLimit: 1,
		SynologyFactory: func(*protocol.ConnectionInfo) (SynologyClient, error) {
			return nil, errors.New("boom")
		},
		Logger: logger,
	})

	err := d.Run(&protocol.ConnectionInfo{})
	if err == nil || err.Error() != "fail to make synology client: boom" {
		t.Fatalf("Run() error = %v", err)
	}
	if logger.ContainsExact("Done!") {
		t.Fatal("Run() logged Done! on factory error")
	}
}

func TestDownloaderRun_SearchErrorWrapping(t *testing.T) {
	t.Parallel()

	client := &testSynologyClient{
		getFileListErr: map[string]error{"/root": errors.New("list boom")},
	}
	logger := &testLogger{}
	d := NewDownloader(DownloadOptions{
		RootRemotePath:   "/root",
		MetadataFilename: "metadata.yaml",
		WorkerLimit:      1,
		SynologyFactory:  newTestSynologyFactory(client).Fn(),
		MetadataStore:    newTestMetadataStore(),
		Logger:           logger,
	})

	err := d.Run(&protocol.ConnectionInfo{})
	if err == nil || err.Error() != "fail to search from synology filestation: list boom" {
		t.Fatalf("Run() error = %v", err)
	}
	if logger.ContainsExact("Done!") {
		t.Fatal("Run() logged Done! on search error")
	}
}

func TestDownloaderRun_NonPositiveWorkerLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int64
	}{
		{name: "zero", limit: 0},
		{name: "negative", limit: -1},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &testSynologyClient{}
			factory := newTestSynologyFactory(client)
			logger := &testLogger{}
			d := NewDownloader(DownloadOptions{
				WorkerLimit:     tt.limit,
				SynologyFactory: factory.Fn(),
				Logger:          logger,
			})

			err := d.Run(&protocol.ConnectionInfo{})
			if err == nil || err.Error() != "download worker must be positive" {
				t.Fatalf("Run() error = %v", err)
			}
			if got := factory.CallCount(); got != 0 {
				t.Fatalf("synology factory calls = %d, want 0", got)
			}
			if got := client.GetFileListCalls(); len(got) != 0 {
				t.Fatalf("GetFileList calls = %#v, want none", got)
			}
			if got := client.DownloadCalls(); len(got) != 0 {
				t.Fatalf("DownloadFile calls = %#v, want none", got)
			}
			if logger.ContainsExact("Done!") {
				t.Fatal("Run() logged Done! for non-positive worker limit")
			}
		})
	}
}

func TestNewDownloader_PreservesConfiguredWorkerLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int64
	}{
		{name: "positive", limit: 3},
		{name: "zero", limit: 0},
		{name: "negative", limit: -2},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := NewDownloader(DownloadOptions{WorkerLimit: tt.limit})
			if d.workerLimit != tt.limit {
				t.Fatalf("workerLimit = %d, want %d", d.workerLimit, tt.limit)
			}
		})
	}
}

func TestNewDownloader_PreservesExcludePaths(t *testing.T) {
	t.Parallel()

	excludePaths := []string{"/root/private", "/root/secret.jpg"}
	d := NewDownloader(DownloadOptions{ExcludePaths: excludePaths})

	if !reflect.DeepEqual(d.excludePaths, excludePaths) {
		t.Fatalf("excludePaths = %#v, want %#v", d.excludePaths, excludePaths)
	}
}

func TestDownloaderSearchSynologyRecursive_ExcludedRootDoesNotList(t *testing.T) {
	t.Parallel()

	client := &testSynologyClient{}
	d := NewDownloader(DownloadOptions{ExcludePaths: []string{"/root"}})

	resp, err := d.searchSynologyRecursive(client, "/root", 0)
	if err != nil {
		t.Fatalf("searchSynologyRecursive() error = %v", err)
	}
	if resp == nil || !resp.Success || resp.Data.Total != 0 || len(resp.Data.Files) != 0 {
		t.Fatalf("searchSynologyRecursive() response = %+v, want empty success", resp)
	}
	if got := client.GetFileListCalls(); len(got) != 0 {
		t.Fatalf("GetFileList calls = %#v, want none", got)
	}
}

func TestDownloaderSearchSynologyRecursive_ExcludedDirectoryPrunesEffectsButKeepsSiblingPrefix(t *testing.T) {
	t.Parallel()

	client := &testSynologyClient{
		fileLists: map[string]*protocol.FileListResponse{
			"/root": response(
				dir("private", "/root/private"),
				dir("private2", "/root/private2"),
			),
			"/root/private":  response(file("hidden.jpg", "/root/private/hidden.jpg", 1)),
			"/root/private2": response(file("visible.jpg", "/root/private2/visible.jpg", 2)),
		},
	}
	store := newTestMetadataStore()
	var mkdirCalls []string
	d := NewDownloader(DownloadOptions{
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		ExcludePaths:     []string{"/root/private"},
		MkdirAll: func(path string, _ os.FileMode) error {
			mkdirCalls = append(mkdirCalls, path)
			return nil
		},
		MetadataStore: store,
		Logger:        &testLogger{},
	})

	_, err := d.searchSynologyRecursive(client, "/root", 0)
	if err != nil {
		t.Fatalf("searchSynologyRecursive() error = %v", err)
	}
	if got, want := client.GetFileListCalls(), []string{"/root", "/root/private2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("GetFileList calls = %#v, want %#v", got, want)
	}
	if got, want := mkdirCalls, []string{filepath.Join("/download", "/root/private2")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mkdir calls = %#v, want %#v", got, want)
	}
	writes := store.WriteCalls()
	if len(writes) != 1 || writes[0].filePath != filepath.Join("/download", "/root/private2/visible.jpg") {
		t.Fatalf("metadata writes = %#v, want only private2 file", writes)
	}
}

func TestDownloaderSearchSynologyRecursive_ExcludedFileSkipsMetadata(t *testing.T) {
	t.Parallel()

	client := &testSynologyClient{fileLists: map[string]*protocol.FileListResponse{
		"/root": response(
			file("secret.jpg", "/root/secret.jpg", 1),
			file("public.jpg", "/root/public.jpg", 2),
		),
	}}
	store := newTestMetadataStore()
	d := NewDownloader(DownloadOptions{
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		ExcludePaths:     []string{"/root/secret.jpg"},
		MetadataStore:    store,
		Logger:           &testLogger{},
	})

	_, err := d.searchSynologyRecursive(client, "/root", 0)
	if err != nil {
		t.Fatalf("searchSynologyRecursive() error = %v", err)
	}
	writes := store.WriteCalls()
	if len(writes) != 1 || writes[0].filePath != filepath.Join("/download", "/root/public.jpg") {
		t.Fatalf("metadata writes = %#v, want only public file", writes)
	}
}

func TestDownloaderDownloadSynologyRecursive_PrebuiltTreeSkipsExcludedPaths(t *testing.T) {
	t.Parallel()

	client := &testSynologyClient{}
	d := NewDownloader(DownloadOptions{
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		ExcludePaths:     []string{"/root/private", "/root/secret.jpg"},
		MetadataStore:    newTestMetadataStore(),
		Logger:           &testLogger{},
	})
	tree := response(
		dir("private", "/root/private"),
		dir("private2", "/root/private2"),
		file("secret.jpg", "/root/secret.jpg", 1),
		file("public.jpg", "/root/public.jpg", 1),
	)
	tree.Data.Files[0].List = response(file("hidden.jpg", "/root/private/hidden.jpg", 1))
	tree.Data.Files[1].List = response(file("visible.jpg", "/root/private2/visible.jpg", 1))
	sem := semaphore.NewWeighted(1)
	wg := &sync.WaitGroup{}

	if err := d.downloadSynologyRecursive(client, tree, sem, wg, &downloadWorkerFirstError{}); err != nil {
		t.Fatalf("downloadSynologyRecursive() error = %v", err)
	}
	wg.Wait()
	got := client.DownloadCalls()
	want := []downloadCall{
		{filePath: "/root/private2/visible.jpg", destPath: filepath.Join("/download", "/root/private2/visible.jpg")},
		{filePath: "/root/public.jpg", destPath: filepath.Join("/download", "/root/public.jpg")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("download calls = %#v, want %#v", got, want)
	}
}

func TestDownloaderSearchSynologyRecursive_EmptyExcludePathsPreservesBehavior(t *testing.T) {
	t.Parallel()

	for _, excludePaths := range [][]string{nil, {}} {
		client := &testSynologyClient{fileLists: map[string]*protocol.FileListResponse{
			"/root": response(
				dir("private", "/root/private"),
				file("public.jpg", "/root/public.jpg", 1),
			),
			"/root/private": response(file("hidden.jpg", "/root/private/hidden.jpg", 2)),
		}}
		store := newTestMetadataStore()
		d := NewDownloader(DownloadOptions{
			LocalPath:        "/download",
			MetadataFilename: "metadata.yaml",
			ExcludePaths:     excludePaths,
			MkdirAll:         func(string, os.FileMode) error { return nil },
			MetadataStore:    store,
			Logger:           &testLogger{},
		})

		_, err := d.searchSynologyRecursive(client, "/root", 0)
		if err != nil {
			t.Fatalf("searchSynologyRecursive(%#v) error = %v", excludePaths, err)
		}
		if got, want := client.GetFileListCalls(), []string{"/root", "/root/private"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("GetFileList calls with %#v = %#v, want %#v", excludePaths, got, want)
		}
		if got := len(store.WriteCalls()); got != 2 {
			t.Fatalf("metadata writes with %#v = %d, want 2", excludePaths, got)
		}
	}
}

func TestDownloaderSearchSynologyRecursive_CreatesDirectoriesAndSkipsRecycle(t *testing.T) {
	t.Parallel()

	client := &testSynologyClient{
		fileLists: map[string]*protocol.FileListResponse{
			"/root": response(
				dir("album", "/root/album"),
				dir("#recycle", "/root/#recycle"),
				file("a.jpg", "/root/a.jpg", 10),
			),
			"/root/album": response(
				dir("nested", "/root/album/nested"),
				file("b.jpg", "/root/album/b.jpg", 20),
			),
			"/root/album/nested": response(),
		},
	}
	store := newTestMetadataStore()
	logger := &testLogger{}
	var mkdirCalls []string
	var mkdirMu sync.Mutex
	d := NewDownloader(DownloadOptions{
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		MkdirAll: func(path string, _ os.FileMode) error {
			mkdirMu.Lock()
			defer mkdirMu.Unlock()
			mkdirCalls = append(mkdirCalls, path)
			return nil
		},
		MetadataStore: store,
		Logger:        logger,
	})

	resp, err := d.searchSynologyRecursive(client, "/root", 0)
	if err != nil {
		t.Fatalf("searchSynologyRecursive() error = %v", err)
	}
	if resp == nil || resp.Data.Total != 3 {
		t.Fatalf("searchSynologyRecursive() returned unexpected response: %+v", resp)
	}

	wantMkdirs := []string{
		filepath.Join("/download", "/root/album"),
		filepath.Join("/download", "/root/album/nested"),
	}
	if !reflect.DeepEqual(mkdirCalls, wantMkdirs) {
		t.Fatalf("mkdir calls = %#v, want %#v", mkdirCalls, wantMkdirs)
	}

	if got := client.GetFileListCalls(); !reflect.DeepEqual(got, []string{"/root", "/root/album", "/root/album/nested"}) {
		t.Fatalf("GetFileList calls = %#v", got)
	}

	writes := store.WriteCalls()
	if len(writes) != 2 {
		t.Fatalf("write calls = %d, want 2", len(writes))
	}
	gotWrites := make(map[string]metadataWriteCall, len(writes))
	for _, write := range writes {
		gotWrites[write.filePath] = write
	}
	assertMetadataWrite(t, gotWrites[filepath.Join("/download", "/root/a.jpg")], filepath.Join("/download", "/root/a.jpg"), "metadata.yaml", 10, protocol.Init)
	assertMetadataWrite(t, gotWrites[filepath.Join("/download", "/root/album/b.jpg")], filepath.Join("/download", "/root/album/b.jpg"), "metadata.yaml", 20, protocol.Init)
	if logger.Contains("#recycle") {
		t.Fatal("unexpected recycle log entry")
	}
}

func TestDownloaderSearchSynologyRecursive_MkdirError(t *testing.T) {
	t.Parallel()

	d := NewDownloader(DownloadOptions{
		LocalPath: "/download",
		MkdirAll: func(string, os.FileMode) error {
			return errors.New("mkdir boom")
		},
		MetadataStore: newTestMetadataStore(),
		Logger:        &testLogger{},
	})

	_, err := d.searchSynologyRecursive(&testSynologyClient{
		fileLists: map[string]*protocol.FileListResponse{
			"/root": response(dir("album", "/root/album")),
		},
	}, "/root", 0)
	if err == nil || err.Error() != "fail to make download folder: mkdir boom" {
		t.Fatalf("searchSynologyRecursive() error = %v", err)
	}
}

func TestDownloaderInitializeMetadata_MissingMetadataFileWritesInitWithoutRemove(t *testing.T) {
	t.Parallel()

	store := newTestMetadataStore()
	logger := &testLogger{}
	removed := false
	d := NewDownloader(DownloadOptions{
		MetadataFilename: "metadata.yaml",
		MetadataStore:    store,
		Remove: func(string) error {
			removed = true
			return nil
		},
		Logger: logger,
	})

	filePath := filepath.Join("/download", "/root/a.jpg")
	err := d.initializeMetadata(filePath, 55)
	if err != nil {
		t.Fatalf("initializeMetadata() error = %v", err)
	}

	writes := store.WriteCalls()
	if len(writes) != 1 {
		t.Fatalf("write calls = %d, want 1", len(writes))
	}
	assertMetadataWrite(t, writes[0], filePath, "metadata.yaml", 55, protocol.Init)
	if removed {
		t.Fatal("initializeMetadata() removed file when metadata file was missing")
	}
	if !logger.Contains(fmt.Sprintf("init %s metadata", filePath)) {
		t.Fatalf("missing init log: %#v", logger.Entries())
	}
}

func TestDownloaderInitializeMetadata_ReinitializesMissingOrMismatchedEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]protocol.FileMetadata
		size     uint64
	}{
		{
			name:     "missing entry",
			metadata: map[string]protocol.FileMetadata{},
			size:     10,
		},
		{
			name: "size mismatch",
			metadata: map[string]protocol.FileMetadata{
				filepath.Join("/download", "/root/a.jpg"): {Size: 9, Status: string(protocol.Sent)},
			},
			size: 10,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filePath := filepath.Join("/download", "/root/a.jpg")
			metadataPath := filepath.Join(filepath.Dir(filePath), "metadata.yaml")
			store := newTestMetadataStore()
			store.SetExists(metadataPath, true)
			store.SetExists(filePath, true)
			store.SetRead(filepath.Dir(filePath), "metadata.yaml", tt.metadata)
			logger := &testLogger{}
			var removed []string
			var removedMu sync.Mutex
			d := NewDownloader(DownloadOptions{
				MetadataFilename: "metadata.yaml",
				MetadataStore:    store,
				Remove: func(path string) error {
					removedMu.Lock()
					defer removedMu.Unlock()
					removed = append(removed, path)
					return nil
				},
				Logger: logger,
			})

			err := d.initializeMetadata(filePath, tt.size)
			if err != nil {
				t.Fatalf("initializeMetadata() error = %v", err)
			}

			writes := store.WriteCalls()
			if len(writes) != 1 {
				t.Fatalf("write calls = %d, want 1", len(writes))
			}
			assertMetadataWrite(t, writes[0], filePath, "metadata.yaml", tt.size, protocol.Init)
			if !reflect.DeepEqual(removed, []string{filePath}) {
				t.Fatalf("remove calls = %#v", removed)
			}
			if !logger.Contains(fmt.Sprintf("init %s metadata", filePath)) {
				t.Fatalf("missing init log: %#v", logger.Entries())
			}
			if !logger.Contains(fmt.Sprintf("remove %s file", filePath)) {
				t.Fatalf("missing remove log: %#v", logger.Entries())
			}
		})
	}
}

func TestDownloaderInitializeMetadata_PreservesMatchingMetadataAndLogsAlreadyExists(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join("/download", "/root/a.jpg")
	store := newTestMetadataStore()
	store.SetExists(filepath.Join(filepath.Dir(filePath), "metadata.yaml"), true)
	store.SetRead(filepath.Dir(filePath), "metadata.yaml", map[string]protocol.FileMetadata{
		filePath: {Size: 10, Status: string(protocol.Sent)},
	})
	logger := &testLogger{}
	removed := false
	d := NewDownloader(DownloadOptions{
		MetadataFilename: "metadata.yaml",
		MetadataStore:    store,
		Remove: func(string) error {
			removed = true
			return nil
		},
		Logger: logger,
	})

	err := d.initializeMetadata(filePath, 10)
	if err != nil {
		t.Fatalf("initializeMetadata() error = %v", err)
	}
	if len(store.WriteCalls()) != 0 {
		t.Fatalf("unexpected write calls: %#v", store.WriteCalls())
	}
	if removed {
		t.Fatal("unexpected remove call")
	}
	wantLog := fmt.Sprintf("%s metadata already exists", filePath)
	if !logger.ContainsExact(wantLog) {
		t.Fatalf("logs = %#v, want %q", logger.Entries(), wantLog)
	}
}

func TestDownloaderInitializeMetadata_ErrorPaths(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join("/download", "/root/a.jpg")
	metadataDir := filepath.Dir(filePath)
	metadataPath := filepath.Join(metadataDir, "metadata.yaml")

	tests := []struct {
		name      string
		configure func(*testMetadataStore)
		remove    func(string) error
		wantErr   string
	}{
		{
			name: "read error",
			configure: func(store *testMetadataStore) {
				store.SetExists(metadataPath, true)
				store.SetReadError(metadataDir, "metadata.yaml", errors.New("read boom"))
			},
			wantErr: "read boom",
		},
		{
			name: "write error on missing metadata file",
			configure: func(store *testMetadataStore) {
				store.SetWriteError(filePath, errors.New("write boom"))
			},
			wantErr: fmt.Sprintf("fail to %s write metadata: write boom", filePath),
		},
		{
			name: "remove error",
			configure: func(store *testMetadataStore) {
				store.SetExists(metadataPath, true)
				store.SetExists(filePath, true)
				store.SetRead(metadataDir, "metadata.yaml", map[string]protocol.FileMetadata{})
			},
			remove: func(string) error {
				return errors.New("remove boom")
			},
			wantErr: fmt.Sprintf("fail to %s remove file: remove boom", filePath),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newTestMetadataStore()
			tt.configure(store)
			d := NewDownloader(DownloadOptions{
				MetadataFilename: "metadata.yaml",
				MetadataStore:    store,
				Remove:           tt.remove,
				Logger:           &testLogger{},
			})

			err := d.initializeMetadata(filePath, 10)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("initializeMetadata() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestDownloaderDownloadFile_SkipsNonInitStatuses(t *testing.T) {
	t.Parallel()

	for _, status := range []protocol.FileTransferStatus{protocol.NotSent, protocol.Sent, protocol.Failed} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			client := &testSynologyClient{}
			store := newTestMetadataStore()
			logger := &testLogger{}
			filePath := filepath.Join("/download", "/root/a.jpg")
			store.SetRead(filepath.Dir(filePath), "metadata.yaml", map[string]protocol.FileMetadata{
				filePath: {Size: 10, Status: string(status)},
			})
			d := NewDownloader(DownloadOptions{
				LocalPath:        "/download",
				MetadataFilename: "metadata.yaml",
				MetadataStore:    store,
				Logger:           logger,
			})

			err := d.downloadFile(client, "/root/a.jpg")
			if err != nil {
				t.Fatalf("downloadFile() error = %v", err)
			}
			if len(client.DownloadCalls()) != 0 {
				t.Fatalf("unexpected download calls: %#v", client.DownloadCalls())
			}
			if len(store.WriteCalls()) != 0 {
				t.Fatalf("unexpected metadata writes: %#v", store.WriteCalls())
			}
			wantLog := fmt.Sprintf("%s has already been downloaded", filePath)
			if !logger.ContainsExact(wantLog) {
				t.Fatalf("logs = %#v, want %q", logger.Entries(), wantLog)
			}
		})
	}
}

func TestDownloaderDownloadFile_DownloadsInitAndMissingMetadataEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]protocol.FileMetadata
	}{
		{
			name: "init metadata",
			metadata: map[string]protocol.FileMetadata{
				filepath.Join("/download", "/root/a.jpg"): {Size: 10, Status: string(protocol.Init)},
			},
		},
		{
			name:     "missing metadata entry",
			metadata: map[string]protocol.FileMetadata{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &testSynologyClient{
				downloadResult: map[string]downloadResult{
					"/root/a.jpg": {path: filepath.Join("/download", "/root/a.jpg")},
				},
			}
			store := newTestMetadataStore()
			filePath := filepath.Join("/download", "/root/a.jpg")
			store.SetRead(filepath.Dir(filePath), "metadata.yaml", tt.metadata)
			logger := &testLogger{}
			d := NewDownloader(DownloadOptions{
				LocalPath:        "/download",
				MetadataFilename: "metadata.yaml",
				MetadataStore:    store,
				Logger:           logger,
			})

			err := d.downloadFile(client, "/root/a.jpg")
			if err != nil {
				t.Fatalf("downloadFile() error = %v", err)
			}

			if got := client.DownloadCalls(); !reflect.DeepEqual(got, []downloadCall{{filePath: "/root/a.jpg", destPath: filePath}}) {
				t.Fatalf("download calls = %#v", got)
			}
			writes := store.WriteCalls()
			if len(writes) != 1 {
				t.Fatalf("write calls = %d, want 1", len(writes))
			}
			assertMetadataWrite(t, writes[0], filePath, "metadata.yaml", 0, protocol.NotSent)
			if !logger.ContainsExact(fmt.Sprintf("%s success download", filePath)) {
				t.Fatalf("logs = %#v", logger.Entries())
			}
		})
	}
}

func TestDownloaderDownloadFile_ErrorPaths(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join("/download", "/root/a.jpg")

	tests := []struct {
		name    string
		store   func() *testMetadataStore
		client  *testSynologyClient
		wantErr string
	}{
		{
			name: "metadata read error",
			store: func() *testMetadataStore {
				store := newTestMetadataStore()
				store.SetReadError(filepath.Dir(filePath), "metadata.yaml", errors.New("read boom"))
				return store
			},
			client:  &testSynologyClient{},
			wantErr: "read boom",
		},
		{
			name: "download error",
			store: func() *testMetadataStore {
				store := newTestMetadataStore()
				store.SetRead(filepath.Dir(filePath), "metadata.yaml", map[string]protocol.FileMetadata{})
				return store
			},
			client: &testSynologyClient{
				downloadErr: map[string]error{"/root/a.jpg": errors.New("download boom")},
			},
			wantErr: fmt.Sprintf("fail to %s download file: download boom", "/root/a.jpg"),
		},
		{
			name: "metadata write error after download",
			store: func() *testMetadataStore {
				store := newTestMetadataStore()
				store.SetRead(filepath.Dir(filePath), "metadata.yaml", map[string]protocol.FileMetadata{})
				store.SetWriteError(filePath, errors.New("write boom"))
				return store
			},
			client: &testSynologyClient{
				downloadResult: map[string]downloadResult{
					"/root/a.jpg": {path: filePath},
				},
			},
			wantErr: fmt.Sprintf("fail to %s write metadata: write boom", filePath),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := &testLogger{}
			d := NewDownloader(DownloadOptions{
				LocalPath:        "/download",
				MetadataFilename: "metadata.yaml",
				MetadataStore:    tt.store(),
				Logger:           logger,
			})

			err := d.downloadFile(tt.client, "/root/a.jpg")
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("downloadFile() error = %v, want %q", err, tt.wantErr)
			}
			if logger.ContainsExact("Done!") {
				t.Fatal("unexpected Done! log")
			}
		})
	}
}

func TestDownloaderRun_WorkerConcurrencyLimit(t *testing.T) {
	t.Parallel()

	gate := newDownloadGate()
	client := &testSynologyClient{
		fileLists: map[string]*protocol.FileListResponse{
			"/root": response(
				file("a.jpg", "/root/a.jpg", 1),
				file("b.jpg", "/root/b.jpg", 1),
				file("c.jpg", "/root/c.jpg", 1),
			),
		},
		downloadResult: map[string]downloadResult{
			"/root/a.jpg": {path: filepath.Join("/download", "/root/a.jpg")},
			"/root/b.jpg": {path: filepath.Join("/download", "/root/b.jpg")},
			"/root/c.jpg": {path: filepath.Join("/download", "/root/c.jpg")},
		},
		downloadGate: gate,
	}
	store := newTestMetadataStore()
	logger := &testLogger{}
	d := NewDownloader(DownloadOptions{
		RootRemotePath:   "/root",
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		WorkerLimit:      2,
		SynologyFactory:  newTestSynologyFactory(client).Fn(),
		MetadataStore:    store,
		Logger:           logger,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(&protocol.ConnectionInfo{})
	}()

	gate.WaitForAtLeast(t, 2)
	if got := gate.Max(); got != 2 {
		t.Fatalf("max concurrent downloads = %d, want 2", got)
	}
	assertNoResult(t, errCh)
	gate.ReleaseAll()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run() did not complete after releasing downloads")
	}

	if !logger.ContainsExact("Done!") {
		t.Fatalf("logs = %#v", logger.Entries())
	}
}

func TestDownloaderRun_WaitsForWorkersBeforeDone(t *testing.T) {
	t.Parallel()

	gate := newDownloadGate()
	client := &testSynologyClient{
		fileLists: map[string]*protocol.FileListResponse{
			"/root": response(file("a.jpg", "/root/a.jpg", 1)),
		},
		downloadResult: map[string]downloadResult{
			"/root/a.jpg": {path: filepath.Join("/download", "/root/a.jpg")},
		},
		downloadGate: gate,
	}
	logger := &testLogger{}
	d := NewDownloader(DownloadOptions{
		RootRemotePath:   "/root",
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		WorkerLimit:      1,
		SynologyFactory:  newTestSynologyFactory(client).Fn(),
		MetadataStore:    newTestMetadataStore(),
		Logger:           logger,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(&protocol.ConnectionInfo{})
	}()

	gate.WaitForAtLeast(t, 1)
	assertNoResult(t, errCh)
	if logger.ContainsExact("Done!") {
		t.Fatalf("logs before release = %#v", logger.Entries())
	}

	gate.ReleaseAll()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run() did not return after release")
	}

	if !logger.ContainsExact(filepath.Join("/download", "/root/a.jpg") + " success download") {
		t.Fatalf("logs = %#v", logger.Entries())
	}
	if !logger.ContainsExact("Done!") {
		t.Fatalf("logs = %#v", logger.Entries())
	}
}

func TestDownloaderRun_ConcurrentInvocationsDoNotShareWaitGroupOrSemaphore(t *testing.T) {
	t.Parallel()

	gateA := newDownloadGate()
	gateB := newDownloadGate()
	clientA := &testSynologyClient{
		fileLists: map[string]*protocol.FileListResponse{
			"/root": response(file("a.jpg", "/root/a.jpg", 1)),
		},
		downloadResult: map[string]downloadResult{
			"/root/a.jpg": {path: filepath.Join("/download", "/root/a.jpg")},
		},
		downloadGate: gateA,
	}
	clientB := &testSynologyClient{
		fileLists: map[string]*protocol.FileListResponse{
			"/root": response(file("b.jpg", "/root/b.jpg", 1)),
		},
		downloadResult: map[string]downloadResult{
			"/root/b.jpg": {path: filepath.Join("/download", "/root/b.jpg")},
		},
		downloadGate: gateB,
	}
	factory := newTestSynologyFactory(clientA, clientB)
	logger := &testLogger{}
	d := NewDownloader(DownloadOptions{
		RootRemotePath:   "/root",
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		WorkerLimit:      1,
		SynologyFactory:  factory.Fn(),
		MetadataStore:    newTestMetadataStore(),
		Logger:           logger,
	})

	errCh := make(chan error, 2)
	go func() { errCh <- d.Run(&protocol.ConnectionInfo{}) }()
	go func() { errCh <- d.Run(&protocol.ConnectionInfo{}) }()

	gateA.WaitForAtLeast(t, 1)
	gateB.WaitForAtLeast(t, 1)
	assertNoResult(t, errCh)
	if gateA.Max() != 1 || gateB.Max() != 1 {
		t.Fatalf("gates max = %d and %d, want both 1", gateA.Max(), gateB.Max())
	}

	gateA.ReleaseAll()
	gateB.ReleaseAll()

	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("concurrent Run() calls did not finish")
		}
	}

	if factory.CallCount() != 2 {
		t.Fatalf("factory call count = %d, want 2", factory.CallCount())
	}
	if logger.CountExact("Done!") != 2 {
		t.Fatalf("Done! count = %d, logs = %#v", logger.CountExact("Done!"), logger.Entries())
	}
}

func TestDownloaderRun_WorkerErrorHandlerCalledBeforeRunReturns(t *testing.T) {
	t.Parallel()

	blockedRelease := make(chan struct{})
	allowHandlerReturn := make(chan struct{})
	t.Cleanup(func() {
		closeOnce(blockedRelease)
		closeOnce(allowHandlerReturn)
	})

	workerErr := errors.New("worker boom")
	handlerCalled := make(chan error, 1)
	client := newWorkerErrorTestClient(workerErr, blockedRelease)
	d := NewDownloader(DownloadOptions{
		RootRemotePath:   "/root",
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		WorkerLimit:      2,
		SynologyFactory:  newTestSynologyFactory(client).Fn(),
		MetadataStore:    newTestMetadataStore(),
		Logger:           &testLogger{},
		WorkerErrorHandler: func(err error) {
			handlerCalled <- err
			<-allowHandlerReturn
		},
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(&protocol.ConnectionInfo{})
	}()

	client.WaitForBlockedWorker(t)
	var gotHandlerErr error
	select {
	case gotErr := <-handlerCalled:
		gotHandlerErr = gotErr
		if gotErr == nil || gotErr.Error() != "fail to /root/a.jpg download file: worker boom" {
			t.Fatalf("handler error = %v", gotErr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for worker error handler")
	}
	assertNoResult(t, errCh)
	closeOnce(blockedRelease)
	assertNoResult(t, errCh)
	closeOnce(allowHandlerReturn)

	select {
	case err := <-errCh:
		if err != gotHandlerErr {
			t.Fatalf("Run() error = %v, want exact handler error %v", err, gotHandlerErr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run() did not return after releasing blocked worker")
	}
}

func TestDownloaderRun_WorkerErrorHandlerNilKeepsNormalBehavior(t *testing.T) {
	t.Parallel()

	workerErr := errors.New("worker boom")
	client := newWorkerErrorTestClient(workerErr, nil)
	d := NewDownloader(DownloadOptions{
		RootRemotePath:   "/root",
		LocalPath:        "/download",
		MetadataFilename: "metadata.yaml",
		WorkerLimit:      2,
		SynologyFactory:  newTestSynologyFactory(client).Fn(),
		MetadataStore:    newTestMetadataStore(),
		Logger:           &testLogger{},
	})

	err := d.Run(&protocol.ConnectionInfo{})
	if err == nil || err.Error() != "fail to /root/a.jpg download file: worker boom" {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestDownloadWorkerFirstError(t *testing.T) {
	t.Parallel()

	first := errors.New("first")
	later := errors.New("later")
	errState := &downloadWorkerFirstError{}

	errState.Add(nil)
	if err := errState.Err(); err != nil {
		t.Fatalf("Err() after nil Add = %v, want nil", err)
	}

	errState.Add(first)
	if err := errState.Err(); !errors.Is(err, first) {
		t.Fatalf("Err() after first Add = %v, want %v", err, first)
	}

	errState.Add(later)
	if err := errState.Err(); !errors.Is(err, first) {
		t.Fatalf("Err() after later Add = %v, want %v", err, first)
	}
}

type metadataWriteCall struct {
	filePath string
	filename string
	size     uint64
	status   protocol.FileTransferStatus
}

type testMetadataStore struct {
	mu         sync.Mutex
	readData   map[string]map[string]protocol.FileMetadata
	readErr    map[string]error
	writeErr   map[string]error
	exists     map[string]bool
	writeCalls []metadataWriteCall
}

func newTestMetadataStore() *testMetadataStore {
	return &testMetadataStore{
		readData: make(map[string]map[string]protocol.FileMetadata),
		readErr:  make(map[string]error),
		writeErr: make(map[string]error),
		exists:   make(map[string]bool),
	}
}

func (s *testMetadataStore) Read(folderPath, filename string) (map[string]protocol.FileMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := filepath.Join(folderPath, filename)
	if err := s.readErr[key]; err != nil {
		return nil, err
	}
	return cloneMetadataMap(s.readData[key]), nil
}

func (s *testMetadataStore) Write(filePath, filename string, size uint64, status protocol.FileTransferStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.writeErr[filePath]; err != nil {
		return err
	}
	s.writeCalls = append(s.writeCalls, metadataWriteCall{filePath: filePath, filename: filename, size: size, status: status})
	metadataPath := filepath.Join(filepath.Dir(filePath), filename)
	s.exists[metadataPath] = true
	if _, ok := s.readData[metadataPath]; !ok {
		s.readData[metadataPath] = make(map[string]protocol.FileMetadata)
	}
	s.readData[metadataPath][filePath] = protocol.FileMetadata{Size: size, Status: string(status)}
	return nil
}

func (s *testMetadataStore) Exists(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exists[path]
}

func (s *testMetadataStore) SetExists(path string, exists bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.exists[path] = exists
}

func (s *testMetadataStore) SetRead(folderPath, filename string, metadata map[string]protocol.FileMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.readData[filepath.Join(folderPath, filename)] = cloneMetadataMap(metadata)
}

func (s *testMetadataStore) SetReadError(folderPath, filename string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.readErr[filepath.Join(folderPath, filename)] = err
}

func (s *testMetadataStore) SetWriteError(filePath string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.writeErr[filePath] = err
}

func (s *testMetadataStore) WriteCalls() []metadataWriteCall {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]metadataWriteCall(nil), s.writeCalls...)
}

type downloadCall struct {
	filePath string
	destPath string
}

type workerErrorTestClient struct {
	workerErr          error
	blockedRelease     <-chan struct{}
	blockedStarted     chan struct{}
	blockedStartedOnce sync.Once
	mu                 sync.Mutex
	downloadCalls      []downloadCall
}

func newWorkerErrorTestClient(workerErr error, blockedRelease <-chan struct{}) *workerErrorTestClient {
	return &workerErrorTestClient{
		workerErr:      workerErr,
		blockedRelease: blockedRelease,
		blockedStarted: make(chan struct{}),
	}
}

func (c *workerErrorTestClient) GetFileList(folderPath string) (*protocol.FileListResponse, error) {
	if folderPath != "/root" {
		return response(), nil
	}

	return response(
		file("a.jpg", "/root/a.jpg", 1),
		file("b.jpg", "/root/b.jpg", 1),
	), nil
}

func (c *workerErrorTestClient) DownloadFile(filePath, destPath string) (string, int64, error) {
	c.mu.Lock()
	c.downloadCalls = append(c.downloadCalls, downloadCall{filePath: filePath, destPath: destPath})
	c.mu.Unlock()

	if filePath == "/root/a.jpg" {
		<-c.blockedStarted
		return "", 0, c.workerErr
	}

	c.blockedStartedOnce.Do(func() {
		close(c.blockedStarted)
	})
	if c.blockedRelease != nil {
		<-c.blockedRelease
	}
	return destPath, 0, nil
}

func (c *workerErrorTestClient) WaitForBlockedWorker(t *testing.T) {
	t.Helper()

	select {
	case <-c.blockedStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for blocked worker")
	}
}

type downloadResult struct {
	path string
	size int64
}

type testSynologyClient struct {
	mu             sync.Mutex
	fileLists      map[string]*protocol.FileListResponse
	getFileListErr map[string]error
	downloadResult map[string]downloadResult
	downloadErr    map[string]error
	downloadGate   *downloadGate
	getCalls       []string
	downloadCalls  []downloadCall
}

func (c *testSynologyClient) GetFileList(folderPath string) (*protocol.FileListResponse, error) {
	c.mu.Lock()
	c.getCalls = append(c.getCalls, folderPath)
	err := c.getFileListErr[folderPath]
	resp := cloneResponse(c.fileLists[folderPath])
	c.mu.Unlock()

	if err != nil {
		return nil, err
	}
	if resp == nil {
		return response(), nil
	}
	return resp, nil
}

func (c *testSynologyClient) DownloadFile(filePath, destPath string) (string, int64, error) {
	c.mu.Lock()
	c.downloadCalls = append(c.downloadCalls, downloadCall{filePath: filePath, destPath: destPath})
	err := c.downloadErr[filePath]
	result, ok := c.downloadResult[filePath]
	gate := c.downloadGate
	c.mu.Unlock()

	if gate != nil {
		gate.Enter()
		defer gate.Exit()
	}
	if err != nil {
		return "", 0, err
	}
	if ok {
		return result.path, result.size, nil
	}
	return destPath, 0, nil
}

func (c *testSynologyClient) GetFileListCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.getCalls...)
}

func (c *testSynologyClient) DownloadCalls() []downloadCall {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]downloadCall(nil), c.downloadCalls...)
}

type testSynologyFactory struct {
	mu      sync.Mutex
	clients []SynologyClient
	err     error
	calls   int
	infos   []*protocol.ConnectionInfo
}

func newTestSynologyFactory(clients ...SynologyClient) *testSynologyFactory {
	return &testSynologyFactory{clients: clients}
}

func (f *testSynologyFactory) Fn() SynologyFactory {
	return func(info *protocol.ConnectionInfo) (SynologyClient, error) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.calls++
		f.infos = append(f.infos, info)
		if f.err != nil {
			return nil, f.err
		}
		idx := f.calls - 1
		if idx >= len(f.clients) {
			idx = len(f.clients) - 1
		}
		if idx < 0 {
			return nil, nil
		}
		return f.clients[idx], nil
	}
}

func (f *testSynologyFactory) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

type testLogger struct {
	mu      sync.Mutex
	entries []string
}

func (l *testLogger) Print(v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.entries = append(l.entries, fmt.Sprint(v...))
}

func (l *testLogger) Printf(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.entries = append(l.entries, fmt.Sprintf(format, v...))
}

func (l *testLogger) Entries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.entries...)
}

func (l *testLogger) Contains(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, entry := range l.entries {
		if strings.Contains(entry, substr) {
			return true
		}
	}
	return false
}

func (l *testLogger) ContainsExact(want string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, entry := range l.entries {
		if entry == want {
			return true
		}
	}
	return false
}

func (l *testLogger) CountExact(want string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := 0
	for _, entry := range l.entries {
		if entry == want {
			count++
		}
	}
	return count
}

type downloadGate struct {
	mu          sync.Mutex
	entered     int
	inFlight    int
	maxInFlight int
	releaseCh   chan struct{}
	signalCh    chan struct{}
}

func newDownloadGate() *downloadGate {
	return &downloadGate{
		releaseCh: make(chan struct{}),
		signalCh:  make(chan struct{}, 32),
	}
}

func (g *downloadGate) Enter() {
	g.mu.Lock()
	g.entered++
	g.inFlight++
	if g.inFlight > g.maxInFlight {
		g.maxInFlight = g.inFlight
	}
	releaseCh := g.releaseCh
	g.mu.Unlock()

	select {
	case g.signalCh <- struct{}{}:
	default:
	}

	<-releaseCh
}

func (g *downloadGate) Exit() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.inFlight--
	if g.inFlight < 0 {
		g.inFlight = 0
	}
}

func (g *downloadGate) WaitForAtLeast(t *testing.T, want int) {
	t.Helper()

	deadline := time.After(200 * time.Millisecond)
	for {
		g.mu.Lock()
		if g.entered >= want {
			g.mu.Unlock()
			return
		}
		g.mu.Unlock()

		select {
		case <-g.signalCh:
		case <-deadline:
			t.Fatalf("timed out waiting for %d download(s), saw %d", want, g.Entered())
		}
	}
}

func (g *downloadGate) ReleaseAll() {
	g.mu.Lock()
	defer g.mu.Unlock()

	select {
	case <-g.releaseCh:
	default:
		close(g.releaseCh)
	}
}

func (g *downloadGate) Max() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.maxInFlight
}

func (g *downloadGate) Entered() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.entered
}

func assertNoResult(t *testing.T, errCh <-chan error) {
	t.Helper()

	select {
	case err := <-errCh:
		t.Fatalf("received early result: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
}

func closeOnce(ch chan struct{}) {
	defer func() {
		_ = recover()
	}()
	close(ch)
}

func assertMetadataWrite(t *testing.T, got metadataWriteCall, wantPath, wantFilename string, wantSize uint64, wantStatus protocol.FileTransferStatus) {
	t.Helper()

	if got.filePath != wantPath || got.filename != wantFilename || got.size != wantSize || got.status != wantStatus {
		t.Fatalf("metadata write = %#v, want path=%q filename=%q size=%d status=%q", got, wantPath, wantFilename, wantSize, wantStatus)
	}
}

func response(files ...*protocol.File) *protocol.FileListResponse {
	resp := &protocol.FileListResponse{}
	resp.Data.Files = files
	resp.Data.Total = len(files)
	resp.Success = true
	return resp
}

func dir(name, path string) *protocol.File {
	return &protocol.File{Name: name, Path: path, IsDir: true}
}

func file(name, path string, size uint64) *protocol.File {
	f := &protocol.File{Name: name, Path: path}
	f.Additional.Size = size
	return f
}

func cloneMetadataMap(src map[string]protocol.FileMetadata) map[string]protocol.FileMetadata {
	if src == nil {
		return map[string]protocol.FileMetadata{}
	}
	dst := make(map[string]protocol.FileMetadata, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneResponse(src *protocol.FileListResponse) *protocol.FileListResponse {
	if src == nil {
		return nil
	}
	dst := &protocol.FileListResponse{}
	dst.Data.Offset = src.Data.Offset
	dst.Data.Total = src.Data.Total
	dst.Success = src.Success
	if len(src.Data.Files) > 0 {
		dst.Data.Files = make([]*protocol.File, 0, len(src.Data.Files))
		for _, f := range src.Data.Files {
			dst.Data.Files = append(dst.Data.Files, cloneFile(f))
		}
	}
	return dst
}

func cloneFile(src *protocol.File) *protocol.File {
	if src == nil {
		return nil
	}
	dst := &protocol.File{
		Name:  src.Name,
		Path:  src.Path,
		IsDir: src.IsDir,
		List:  cloneResponse(src.List),
	}
	dst.Additional.Size = src.Additional.Size
	return dst
}
