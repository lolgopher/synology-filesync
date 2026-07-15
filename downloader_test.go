package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lolgopher/synology-filesync/protocol"
	"golang.org/x/sync/semaphore"
)

func TestIsRecycleDirectory(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "#recycle", want: true},
		{name: "recycle", want: false},
		{name: "#Recycle", want: false},
		{name: "#recycle-child", want: false},
		{name: "", want: false},
	}

	for _, tt := range tests {
		if got := isRecycleDirectory(tt.name); got != tt.want {
			t.Errorf("isRecycleDirectory(%q) = %t, want %t", tt.name, got, tt.want)
		}
	}
}

func TestInitializeMetadataMissingMetadataPreservesPayloadAndUsesConfiguredPathKey(t *testing.T) {
	metadataFilename := configureMetadataFilename(t, "transfer-state.yaml")
	filePath := filepath.Join(t.TempDir(), "payload.bin")
	wantPayload := []byte("existing payload")
	if err := os.WriteFile(filePath, wantPayload, 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	logs := captureLogs(t)

	if err := initializeMetadata(filePath, 42); err != nil {
		t.Fatalf("initialize metadata: %v", err)
	}

	gotPayload, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read preserved payload: %v", err)
	}
	if !bytes.Equal(gotPayload, wantPayload) {
		t.Fatalf("payload = %q, want %q", gotPayload, wantPayload)
	}
	metadata, err := protocol.ReadMetadata(filepath.Dir(filePath), metadataFilename)
	if err != nil {
		t.Fatalf("read initialized metadata: %v", err)
	}
	if len(metadata) != 1 {
		t.Fatalf("metadata entries = %d, want 1: %#v", len(metadata), metadata)
	}
	if got := metadata[filePath]; got.Size != 42 || got.Status != protocol.Init {
		t.Fatalf("metadata[%q] = %#v, want size 42 and status %q", filePath, got, protocol.Init)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filePath), "metadata.yaml")); !os.IsNotExist(err) {
		t.Fatalf("default metadata path unexpectedly exists: %v", err)
	}
	if want := "init " + filePath + " metadata\n"; logs.String() != want {
		t.Fatalf("logs = %q, want %q", logs.String(), want)
	}
}

func TestInitializeMetadataMissingEntryOrSizeMismatchReinitializesAndDeletesPayload(t *testing.T) {
	tests := []struct {
		name string
		seed func(*testing.T, string, string)
	}{
		{
			name: "missing entry",
			seed: func(t *testing.T, filePath, metadataFilename string) {
				otherPath := filepath.Join(filepath.Dir(filePath), "other.bin")
				if err := protocol.WriteMetadata(otherPath, metadataFilename, 42, protocol.Init); err != nil {
					t.Fatalf("seed other metadata entry: %v", err)
				}
			},
		},
		{
			name: "size mismatch",
			seed: func(t *testing.T, filePath, metadataFilename string) {
				if err := protocol.WriteMetadata(filePath, metadataFilename, 41, protocol.Init); err != nil {
					t.Fatalf("seed mismatched metadata entry: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadataFilename := configureMetadataFilename(t, "state.yaml")
			filePath := filepath.Join(t.TempDir(), "payload.bin")
			if err := os.WriteFile(filePath, []byte("existing payload"), 0o600); err != nil {
				t.Fatalf("write payload: %v", err)
			}
			tt.seed(t, filePath, metadataFilename)
			logs := captureLogs(t)

			if err := initializeMetadata(filePath, 42); err != nil {
				t.Fatalf("initialize metadata: %v", err)
			}

			if _, err := os.Stat(filePath); !os.IsNotExist(err) {
				t.Fatalf("payload was not deleted: %v", err)
			}
			metadata, err := protocol.ReadMetadata(filepath.Dir(filePath), metadataFilename)
			if err != nil {
				t.Fatalf("read reinitialized metadata: %v", err)
			}
			if got := metadata[filePath]; got.Size != 42 || got.Status != protocol.Init {
				t.Fatalf("metadata[%q] = %#v, want size 42 and status %q", filePath, got, protocol.Init)
			}
			wantLogs := "init " + filePath + " metadata\n" +
				"remove " + filePath + " file\n"
			if logs.String() != wantLogs {
				t.Fatalf("logs = %q, want %q", logs.String(), wantLogs)
			}
		})
	}
}

func TestInitializeMetadataMatchingSizePreservesPayload(t *testing.T) {
	metadataFilename := configureMetadataFilename(t, "state.yaml")
	filePath := filepath.Join(t.TempDir(), "payload.bin")
	wantPayload := []byte("existing payload")
	if err := os.WriteFile(filePath, wantPayload, 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := protocol.WriteMetadata(filePath, metadataFilename, 42, protocol.Init); err != nil {
		t.Fatalf("seed matching metadata entry: %v", err)
	}
	logs := captureLogs(t)

	if err := initializeMetadata(filePath, 42); err != nil {
		t.Fatalf("initialize metadata: %v", err)
	}

	gotPayload, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read preserved payload: %v", err)
	}
	if !bytes.Equal(gotPayload, wantPayload) {
		t.Fatalf("payload = %q, want %q", gotPayload, wantPayload)
	}
	if want := filePath + " metedata already exist\n"; logs.String() != want {
		t.Fatalf("logs = %q, want %q", logs.String(), want)
	}
}

func TestSearchSynologyRecursiveCreatesFoldersInitializesMetadataAndSkipsExactRecycleDirectory(t *testing.T) {
	localPath := t.TempDir()
	configureDownloaderConfig(t, localPath)
	photoRoot := filepath.Join(localPath, "photo")
	if err := os.MkdirAll(photoRoot, 0o755); err != nil {
		t.Fatalf("create photo root: %v", err)
	}

	var requestedPaths []string
	var requestedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api"); got != "SYNO.FileStation.List" {
			t.Errorf("api = %q, want %q", got, "SYNO.FileStation.List")
		}
		folderPath := r.URL.Query().Get("folder_path")
		requestedMu.Lock()
		requestedPaths = append(requestedPaths, folderPath)
		requestedMu.Unlock()

		switch folderPath {
		case "/photo":
			_, _ = w.Write([]byte(`{"data":{"files":[{"name":"cover.jpg","path":"/photo/cover.jpg","isdir":false,"additional":{"size":21}},{"name":"album","path":"/photo/album","isdir":true,"additional":{"size":0}},{"name":"#recycle","path":"/photo/#recycle","isdir":true,"additional":{"size":0}},{"name":"#recycle-child","path":"/photo/#recycle-child","isdir":true,"additional":{"size":0}}],"offset":0,"total":4},"success":true}`))
		case "/photo/album":
			_, _ = w.Write([]byte(`{"data":{"files":[{"name":"nested.jpg","path":"/photo/album/nested.jpg","isdir":false,"additional":{"size":77}}],"offset":0,"total":1},"success":true}`))
		case "/photo/#recycle-child":
			_, _ = w.Write([]byte(`{"data":{"files":[{"name":"kept.jpg","path":"/photo/#recycle-child/kept.jpg","isdir":false,"additional":{"size":33}}],"offset":0,"total":1},"success":true}`))
		default:
			http.Error(w, "unexpected folder path", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	resp, err := searchSynologyRecursive(synologyClientForServer(t, server), "/photo", 0)
	if err != nil {
		t.Fatalf("searchSynologyRecursive: %v", err)
	}
	if resp == nil || len(resp.Data.Files) != 4 {
		t.Fatalf("response = %#v, want four top-level entries", resp)
	}
	if resp.Data.Files[1].List == nil || len(resp.Data.Files[1].List.Data.Files) != 1 {
		t.Fatalf("album list = %#v, want one nested file", resp.Data.Files[1].List)
	}
	if resp.Data.Files[3].List == nil || len(resp.Data.Files[3].List.Data.Files) != 1 {
		t.Fatalf("recycle-like list = %#v, want one nested file", resp.Data.Files[3].List)
	}

	requestedMu.Lock()
	gotRequests := append([]string(nil), requestedPaths...)
	requestedMu.Unlock()
	if want := []string{"/photo", "/photo/album", "/photo/#recycle-child"}; fmt.Sprint(gotRequests) != fmt.Sprint(want) {
		t.Fatalf("requested folder paths = %v, want %v", gotRequests, want)
	}
	if _, err := os.Stat(filepath.Join(localPath, "photo", "album")); err != nil {
		t.Fatalf("album directory missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localPath, "photo", "#recycle-child")); err != nil {
		t.Fatalf("recycle-like directory missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localPath, "photo", "#recycle")); !os.IsNotExist(err) {
		t.Fatalf("recycle directory unexpectedly exists: %v", err)
	}

	rootMetadata, err := protocol.ReadMetadata(filepath.Join(localPath, "photo"), config.YAML.Filename)
	if err != nil {
		t.Fatalf("read root metadata: %v", err)
	}
	rootTargetPath := filepath.Join(localPath, "/photo/cover.jpg")
	if got := rootMetadata[rootTargetPath]; got.Size != 21 || got.Status != protocol.Init {
		t.Fatalf("metadata[%q] = %#v, want size 21 and status %q", rootTargetPath, got, protocol.Init)
	}

	metadata, err := protocol.ReadMetadata(filepath.Join(localPath, "photo", "album"), config.YAML.Filename)
	if err != nil {
		t.Fatalf("read nested metadata: %v", err)
	}
	targetPath := filepath.Join(localPath, "/photo/album/nested.jpg")
	if got := metadata[targetPath]; got.Size != 77 || got.Status != protocol.Init {
		t.Fatalf("metadata[%q] = %#v, want size 77 and status %q", targetPath, got, protocol.Init)
	}

	recycleLikeMetadata, err := protocol.ReadMetadata(filepath.Join(localPath, "photo", "#recycle-child"), config.YAML.Filename)
	if err != nil {
		t.Fatalf("read recycle-like metadata: %v", err)
	}
	recycleLikeTargetPath := filepath.Join(localPath, "/photo/#recycle-child/kept.jpg")
	if got := recycleLikeMetadata[recycleLikeTargetPath]; got.Size != 33 || got.Status != protocol.Init {
		t.Fatalf("metadata[%q] = %#v, want size 33 and status %q", recycleLikeTargetPath, got, protocol.Init)
	}
}

func TestSearchSynologyRecursiveMalformedListLogsBodyAndReturnsError(t *testing.T) {
	configureDownloaderConfig(t, t.TempDir())
	logs := captureLogs(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	_, err := searchSynologyRecursive(synologyClientForServer(t, server), "/photo", 0)
	if err == nil {
		t.Fatal("searchSynologyRecursive returned nil error for malformed JSON")
	}
	if got := logs.String(); got != "error to unmarshal body data: not-json\n" {
		t.Fatalf("logs = %q, want %q", got, "error to unmarshal body data: not-json\n")
	}
	if !strings.Contains(err.Error(), "fail to unmarshal http://") || !strings.Contains(err.Error(), "/webapi/entry.cgi?") {
		t.Fatalf("error = %q, want the current Synology list unmarshal error", err)
	}
}

func TestSearchSynologyRecursiveFalseSuccessLogsButReturnsResponse(t *testing.T) {
	configureDownloaderConfig(t, t.TempDir())
	logs := captureLogs(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"offset":4,"total":9},"success":false}`))
	}))
	defer server.Close()

	resp, err := searchSynologyRecursive(synologyClientForServer(t, server), "/photo", 0)
	if err != nil {
		t.Fatalf("searchSynologyRecursive: %v", err)
	}
	if resp == nil || resp.Success || resp.Data.Offset != 4 || resp.Data.Total != 9 {
		t.Fatalf("response = %#v, want parsed success=false response", resp)
	}
	if want := fmt.Sprintf("success flag is false: %v\n", resp); logs.String() != want {
		t.Fatalf("logs = %q, want %q", logs.String(), want)
	}
}

func TestDownloadSynologyRecursiveSkipsNonInitWithoutDownloading(t *testing.T) {
	tests := []struct {
		name   string
		status protocol.FileTransferStatus
	}{
		{name: "not sent", status: protocol.NotSent},
		{name: "sent", status: protocol.Sent},
		{name: "failed", status: protocol.Failed},
		{name: "unknown", status: protocol.FileTransferStatus("SOMETHING_ELSE")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localPath := t.TempDir()
			configureDownloaderConfig(t, localPath)
			logs := captureLogs(t)
			configureDownloadGlobals(t, 1)

			dirPath := filepath.Join(localPath, "photo")
			if err := os.MkdirAll(dirPath, 0o755); err != nil {
				t.Fatalf("create directory: %v", err)
			}
			targetPath := filepath.Join(localPath, "/photo/file.jpg")
			if err := protocol.WriteMetadata(targetPath, config.YAML.Filename, 88, tt.status); err != nil {
				t.Fatalf("seed metadata: %v", err)
			}
			beforeMetadata, err := protocol.ReadMetadata(dirPath, config.YAML.Filename)
			if err != nil {
				t.Fatalf("read seeded metadata: %v", err)
			}

			var downloadRequests int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&downloadRequests, 1)
				_, _ = w.Write([]byte("unexpected"))
			}))
			defer server.Close()

			fileList := &protocol.FileListResponse{}
			fileList.Data.Files = []*protocol.File{{Name: "file.jpg", Path: "/photo/file.jpg"}}
			if err := downloadSynologyRecursive(synologyClientForServer(t, server), fileList); err != nil {
				t.Fatalf("downloadSynologyRecursive: %v", err)
			}
			waitForDownloadWorkers(t)

			if got := atomic.LoadInt32(&downloadRequests); got != 0 {
				t.Fatalf("download requests = %d, want 0", got)
			}
			if want := targetPath + " has already been download\n"; logs.String() != want {
				t.Fatalf("logs = %q, want %q", logs.String(), want)
			}
			afterMetadata, err := protocol.ReadMetadata(dirPath, config.YAML.Filename)
			if err != nil {
				t.Fatalf("read resulting metadata: %v", err)
			}
			if fmt.Sprint(afterMetadata) != fmt.Sprint(beforeMetadata) {
				t.Fatalf("metadata changed: got %#v want %#v", afterMetadata, beforeMetadata)
			}
		})
	}
}

func TestDownloadSynologyRecursiveInitDownloadsPayloadAndMarksNotSent(t *testing.T) {
	localPath := t.TempDir()
	configureDownloaderConfig(t, localPath)
	logs := captureLogs(t)
	configureDownloadGlobals(t, 1)

	dirPath := filepath.Join(localPath, "photo")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	targetPath := filepath.Join(localPath, "/photo/file.jpg")
	if err := protocol.WriteMetadata(targetPath, config.YAML.Filename, 88, protocol.Init); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	payload := []byte("downloaded payload")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api"); got != "SYNO.FileStation.Download" {
			t.Errorf("api = %q, want %q", got, "SYNO.FileStation.Download")
		}
		if got := r.URL.Query().Get("path"); got != "/photo/file.jpg" {
			t.Errorf("path = %q, want %q", got, "/photo/file.jpg")
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	fileList := &protocol.FileListResponse{}
	fileList.Data.Files = []*protocol.File{{Name: "file.jpg", Path: "/photo/file.jpg"}}
	if err := downloadSynologyRecursive(synologyClientForServer(t, server), fileList); err != nil {
		t.Fatalf("downloadSynologyRecursive: %v", err)
	}
	waitForDownloadWorkers(t)

	gotPayload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read downloaded payload: %v", err)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload = %q, want %q", gotPayload, payload)
	}
	metadata, err := protocol.ReadMetadata(dirPath, config.YAML.Filename)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if got := metadata[targetPath]; got.Size != 88 || got.Status != protocol.NotSent {
		t.Fatalf("metadata[%q] = %#v, want size 88 and status %q", targetPath, got, protocol.NotSent)
	}
	if want := targetPath + " success download\n"; logs.String() != want {
		t.Fatalf("logs = %q, want %q", logs.String(), want)
	}
}

func TestDownloadSynologyRecursiveHonorsSemaphoreLimit(t *testing.T) {
	localPath := t.TempDir()
	configureDownloaderConfig(t, localPath)
	configureDownloadGlobals(t, 1)

	dirPath := filepath.Join(localPath, "photo")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	filePaths := []string{"/photo/one.jpg", "/photo/two.jpg", "/photo/three.jpg"}
	fileList := &protocol.FileListResponse{}
	for _, filePath := range filePaths {
		targetPath := filepath.Join(localPath, filePath)
		if err := protocol.WriteMetadata(targetPath, config.YAML.Filename, 1, protocol.Init); err != nil {
			t.Fatalf("seed metadata for %s: %v", filePath, err)
		}
		fileList.Data.Files = append(fileList.Data.Files, &protocol.File{Name: filepath.Base(filePath), Path: filePath})
	}

	started := make(chan string, len(filePaths))
	release := make(chan struct{}, len(filePaths))
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	var active int32
	var maxActive int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&active, 1)
		defer atomic.AddInt32(&active, -1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		started <- r.URL.Query().Get("path")
		select {
		case _, ok := <-release:
			if !ok {
				return
			}
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(server.Close)

	done := make(chan error, 1)
	go func() {
		done <- downloadSynologyRecursive(synologyClientForServer(t, server), fileList)
		close(done)
	}()
	t.Cleanup(func() {
		releaseAll()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("timed out waiting for downloadSynologyRecursive to return")
		}
	})

	receiveStarted := func(want string) {
		t.Helper()
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("started path = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q to start", want)
		}
	}
	assertNoStart := func() {
		t.Helper()
		select {
		case got := <-started:
			t.Fatalf("unexpected concurrent start before release: %q", got)
		case <-time.After(150 * time.Millisecond):
		}
	}

	receiveStarted("/photo/one.jpg")
	assertNoStart()
	release <- struct{}{}

	receiveStarted("/photo/two.jpg")
	assertNoStart()
	release <- struct{}{}

	receiveStarted("/photo/three.jpg")
	release <- struct{}{}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("downloadSynologyRecursive: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for downloadSynologyRecursive")
	}
	waitForDownloadWorkers(t)
	if got := atomic.LoadInt32(&maxActive); got != 1 {
		t.Fatalf("max concurrent downloads = %d, want 1", got)
	}
}

func configureMetadataFilename(t *testing.T, filename string) string {
	t.Helper()

	previous := config
	config = &Config{
		YAML: &FileDB{Filename: filename},
	}
	t.Cleanup(func() {
		config = previous
	})
	return filename
}

func configureDownloaderConfig(t *testing.T, localPath string) {
	t.Helper()

	previous := config
	config = &Config{
		YAML:           &FileDB{Filename: "metadata.yaml"},
		LocalPath:      localPath,
		Synology:       &Address{Path: "/photo"},
		DownloadWorker: 1,
	}
	t.Cleanup(func() {
		config = previous
	})
}

func configureDownloadGlobals(t *testing.T, workers int64) {
	t.Helper()

	previousSem := sem
	waitForDownloadWorkers(t)
	sem = semaphore.NewWeighted(workers)
	wg = sync.WaitGroup{}
	t.Cleanup(func() {
		waitForDownloadWorkers(t)
		wg = sync.WaitGroup{}
		sem = previousSem
	})
}

func waitForDownloadWorkers(t *testing.T) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for download workers")
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var logs bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
	return &logs
}

func connectionInfoForServer(t *testing.T, server *httptest.Server) *protocol.ConnectionInfo {
	t.Helper()

	host, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return &protocol.ConnectionInfo{IP: host, Port: port, Username: "user", Password: "password"}
}

func synologyClientForServer(t *testing.T, server *httptest.Server) *protocol.SynologyClient {
	t.Helper()

	return &protocol.SynologyClient{
		ConnInfo: connectionInfoForServer(t, server),
		SessID:   "session-id",
	}
}
