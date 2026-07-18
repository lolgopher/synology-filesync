package app

import (
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lolgopher/synology-filesync/protocol"
	pkgerrors "github.com/pkg/errors"
)

func TestUploaderSearch_SkipsLiteralAndConfiguredMetadataYAML(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	uploadMustWriteFile(t, root, "album/metadata.yaml", []byte("ignored"))
	uploadMustWriteFile(t, root, "album/custom.yaml", []byte("ignored"))

	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(targetPath), "custom.yaml", map[string]protocol.FileMetadata{
		targetPath: {Status: string(protocol.Sent)},
	})
	logger := &uploadTestLogger{}
	sleeper := &uploadTestSleeper{}
	client := &uploadTestSFTPClient{}
	uploader := NewUploader(UploadOptions{LocalPath: root, YAMLFilename: "custom.yaml"}, store, newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
	uploader.client = client

	if err := uploader.Search(root); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := store.ReadCalls(); !reflect.DeepEqual(got, []uploadReadCall{{folderPath: filepath.Dir(targetPath), filename: "custom.yaml"}}) {
		t.Fatalf("read calls = %#v", got)
	}
	if len(store.WriteCalls()) != 0 || len(client.sendCalls) != 0 || len(sleeper.Durations()) != 0 {
		t.Fatalf("unexpected side effects: write=%#v send=%#v sleep=%#v", store.WriteCalls(), client.sendCalls, sleeper.Durations())
	}
	if !logger.ContainsExact(targetPath + " has already been sent") {
		t.Fatalf("logs = %#v", logger.Entries())
	}
}

func TestUploaderSearch_EmptyYAMLFilenameDoesNotSkipPayload(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(targetPath), "", map[string]protocol.FileMetadata{
		targetPath: {Status: string(protocol.Sent)},
	})
	logger := &uploadTestLogger{}
	client := &uploadTestSFTPClient{}
	uploader := NewUploader(UploadOptions{LocalPath: root}, store, newUploadTestSFTPFactory(client).Fn(), logger, nil)
	uploader.client = client

	if err := uploader.Search(root); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := store.ReadCalls(); !reflect.DeepEqual(got, []uploadReadCall{{folderPath: filepath.Dir(targetPath), filename: ""}}) {
		t.Fatalf("read calls = %#v", got)
	}
	if len(store.WriteCalls()) != 0 || len(client.sendCalls) != 0 || len(client.removeCalls) != 0 || len(client.freeSpaceCalls) != 0 {
		t.Fatalf("unexpected side effects: write=%#v send=%#v remove=%#v free=%#v", store.WriteCalls(), client.sendCalls, client.removeCalls, client.freeSpaceCalls)
	}
	if !logger.ContainsExact(targetPath + " has already been sent") {
		t.Fatalf("logs = %#v", logger.Entries())
	}
}

func TestUploaderSearch_StatusLogsAndSkipsWork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  protocol.FileTransferStatus
		wantLog string
	}{
		{name: "init", status: protocol.Init, wantLog: "%s is init metadata status"},
		{name: "sent", status: protocol.Sent, wantLog: "%s has already been sent"},
		{name: "failed", status: protocol.Failed, wantLog: "%s sent failed"},
		{name: "unknown", status: protocol.FileTransferStatus("WHATEVER"), wantLog: "WHATEVER is unknown status"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
			store := newUploadTestMetadataStore()
			store.SetRead(filepath.Dir(targetPath), "metadata.db", map[string]protocol.FileMetadata{
				targetPath: {Status: string(tt.status)},
			})
			logger := &uploadTestLogger{}
			sleeper := &uploadTestSleeper{}
			client := &uploadTestSFTPClient{}
			uploader := NewUploader(UploadOptions{YAMLFilename: "metadata.db"}, store, newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
			uploader.client = client

			if err := uploader.Search(root); err != nil {
				t.Fatalf("Search() error = %v", err)
			}

			wantLog := tt.wantLog
			if strings.Contains(wantLog, "%s") {
				wantLog = fmt.Sprintf(wantLog, targetPath)
			}
			if got := logger.Entries(); !reflect.DeepEqual(got, []string{wantLog}) {
				t.Fatalf("logs = %#v, want %#v", got, []string{wantLog})
			}
			if len(store.WriteCalls()) != 0 || len(sleeper.Durations()) != 0 {
				t.Fatalf("unexpected writes or sleeps: write=%#v sleep=%#v", store.WriteCalls(), sleeper.Durations())
			}
			if len(client.sendCalls) != 0 || len(client.removeCalls) != 0 || len(client.freeSpaceCalls) != 0 {
				t.Fatalf("unexpected sftp calls: send=%#v remove=%#v free=%#v", client.sendCalls, client.removeCalls, client.freeSpaceCalls)
			}
		})
	}
}

func TestUploaderSearch_MissingMetadataEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(targetPath), "metadata.yaml", map[string]protocol.FileMetadata{})
	uploader := NewUploader(UploadOptions{YAMLFilename: "metadata.yaml"}, store, newUploadTestSFTPFactory(&uploadTestSFTPClient{}).Fn(), &uploadTestLogger{}, nil)

	err := uploader.Search(root)
	if err == nil || err.Error() != fmt.Sprintf("fail to find %s in metadata", targetPath) {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestUploaderSearch_MissingRootReturnsWalkError(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "missing")
	uploader := NewUploader(UploadOptions{}, newUploadTestMetadataStore(), newUploadTestSFTPFactory(&uploadTestSFTPClient{}).Fn(), &uploadTestLogger{}, nil)

	err := uploader.Search(root)
	if err == nil {
		t.Fatal("Search() error = nil, want *os.PathError")
	}
	var pathErr *os.PathError
	if !stderrors.As(err, &pathErr) {
		t.Fatalf("Search() error = %T, want *os.PathError", err)
	}
	if pathErr.Path != root || !stderrors.Is(err, os.ErrNotExist) {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestUploaderSearch_ExcludePathsPrunesExcludedDirectoryDescendants(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	visiblePath := uploadMustWriteFile(t, root, filepath.Join("photo", "private2", "visible.jpg"), []byte("visible"))
	hiddenPath := uploadMustWriteFile(t, root, filepath.Join("photo", "private", "nested", "hidden.jpg"), []byte("hidden"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(visiblePath), "metadata.yaml", map[string]protocol.FileMetadata{
		visiblePath: {Status: string(protocol.Sent)},
	})
	store.SetRead(filepath.Dir(hiddenPath), "metadata.yaml", map[string]protocol.FileMetadata{
		hiddenPath: {Status: string(protocol.Sent)},
	})
	logger := &uploadTestLogger{}
	client := &uploadTestSFTPClient{}
	uploader := NewUploader(UploadOptions{
		LocalPath:    root,
		YAMLFilename: "metadata.yaml",
		ExcludePaths: []string{"/photo/private"},
	}, store, newUploadTestSFTPFactory(client).Fn(), logger, nil)
	uploader.client = client

	if err := uploader.Search(filepath.Join(root, "photo")); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := store.ReadCalls(); !reflect.DeepEqual(got, []uploadReadCall{{folderPath: filepath.Dir(visiblePath), filename: "metadata.yaml"}}) {
		t.Fatalf("read calls = %#v", got)
	}
	if len(store.WriteCalls()) != 0 || len(client.sendCalls) != 0 || len(client.freeSpaceCalls) != 0 || len(client.removeCalls) != 0 {
		t.Fatalf("unexpected side effects: write=%#v send=%#v free=%#v remove=%#v", store.WriteCalls(), client.sendCalls, client.freeSpaceCalls, client.removeCalls)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{visiblePath + " has already been sent"}) {
		t.Fatalf("logs = %#v", got)
	}
}

func TestUploaderSearch_ExcludePathsSkipsExactFileBeforeMetadataAndSFTP(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	excludedPath := uploadMustWriteFile(t, root, filepath.Join("photo", "private", "secret.jpg"), []byte("secret"))
	includedPath := uploadMustWriteFile(t, root, filepath.Join("photo", "public.jpg"), []byte("public"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(includedPath), "metadata.yaml", map[string]protocol.FileMetadata{
		includedPath: {Status: string(protocol.Sent)},
	})
	store.SetRead(filepath.Dir(excludedPath), "metadata.yaml", map[string]protocol.FileMetadata{
		excludedPath: {Status: string(protocol.Sent)},
	})
	logger := &uploadTestLogger{}
	client := &uploadTestSFTPClient{}
	uploader := NewUploader(UploadOptions{
		LocalPath:    root,
		YAMLFilename: "metadata.yaml",
		ExcludePaths: []string{"/photo/private/secret.jpg"},
	}, store, newUploadTestSFTPFactory(client).Fn(), logger, nil)
	uploader.client = client

	if err := uploader.Search(filepath.Join(root, "photo")); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := store.ReadCalls(); !reflect.DeepEqual(got, []uploadReadCall{{folderPath: filepath.Dir(includedPath), filename: "metadata.yaml"}}) {
		t.Fatalf("read calls = %#v", got)
	}
	if len(store.WriteCalls()) != 0 || len(client.sendCalls) != 0 || len(client.freeSpaceCalls) != 0 || len(client.removeCalls) != 0 {
		t.Fatalf("unexpected side effects: write=%#v send=%#v free=%#v remove=%#v", store.WriteCalls(), client.sendCalls, client.freeSpaceCalls, client.removeCalls)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{includedPath + " has already been sent"}) {
		t.Fatalf("logs = %#v", got)
	}
}

func TestUploaderSearch_ExcludePathsDoesNotTreatSharedPrefixAsExcluded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	visiblePath := uploadMustWriteFile(t, root, filepath.Join("photo", "private2", "visible.jpg"), []byte("visible"))
	hiddenPath := uploadMustWriteFile(t, root, filepath.Join("photo", "private", "hidden.jpg"), []byte("hidden"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(visiblePath), "metadata.yaml", map[string]protocol.FileMetadata{
		visiblePath: {Status: string(protocol.Sent)},
	})
	store.SetRead(filepath.Dir(hiddenPath), "metadata.yaml", map[string]protocol.FileMetadata{
		hiddenPath: {Status: string(protocol.Sent)},
	})
	logger := &uploadTestLogger{}
	client := &uploadTestSFTPClient{}
	uploader := NewUploader(UploadOptions{
		LocalPath:    root,
		YAMLFilename: "metadata.yaml",
		ExcludePaths: []string{"/photo/private"},
	}, store, newUploadTestSFTPFactory(client).Fn(), logger, nil)
	uploader.client = client

	if err := uploader.Search(filepath.Join(root, "photo")); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := store.ReadCalls(); !reflect.DeepEqual(got, []uploadReadCall{{folderPath: filepath.Dir(visiblePath), filename: "metadata.yaml"}}) {
		t.Fatalf("read calls = %#v", got)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{visiblePath + " has already been sent"}) {
		t.Fatalf("logs = %#v", got)
	}
}

func TestUploaderSearch_ExcludePathsPrunesWalkRootWhenSynologyRootExcluded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	uploadMustWriteFile(t, root, filepath.Join("photo", "private", "hidden.jpg"), []byte("hidden"))
	uploadMustWriteFile(t, root, filepath.Join("photo", "public.jpg"), []byte("public"))
	store := newUploadTestMetadataStore()
	client := &uploadTestSFTPClient{}
	uploader := NewUploader(UploadOptions{
		LocalPath:    root,
		YAMLFilename: "metadata.yaml",
		ExcludePaths: []string{"/photo"},
	}, store, newUploadTestSFTPFactory(client).Fn(), &uploadTestLogger{}, nil)
	uploader.client = client

	if err := uploader.Search(filepath.Join(root, "photo")); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := store.ReadCalls(); len(got) != 0 {
		t.Fatalf("read calls = %#v, want none", got)
	}
	if len(store.WriteCalls()) != 0 || len(client.sendCalls) != 0 || len(client.freeSpaceCalls) != 0 || len(client.removeCalls) != 0 {
		t.Fatalf("unexpected side effects: write=%#v send=%#v free=%#v remove=%#v", store.WriteCalls(), client.sendCalls, client.freeSpaceCalls, client.removeCalls)
	}
}

func TestUploaderSearch_EmptyExcludePathsPreserveBehavior(t *testing.T) {
	t.Parallel()

	for _, excludePaths := range [][]string{nil, {}} {
		root := t.TempDir()
		firstPath := uploadMustWriteFile(t, root, filepath.Join("photo", "private", "hidden.jpg"), []byte("hidden"))
		secondPath := uploadMustWriteFile(t, root, filepath.Join("photo", "private2", "visible.jpg"), []byte("visible"))
		store := newUploadTestMetadataStore()
		store.SetRead(filepath.Dir(firstPath), "metadata.yaml", map[string]protocol.FileMetadata{
			firstPath: {Status: string(protocol.Sent)},
		})
		store.SetRead(filepath.Dir(secondPath), "metadata.yaml", map[string]protocol.FileMetadata{
			secondPath: {Status: string(protocol.Sent)},
		})
		logger := &uploadTestLogger{}
		client := &uploadTestSFTPClient{}
		uploader := NewUploader(UploadOptions{
			LocalPath:    root,
			YAMLFilename: "metadata.yaml",
			ExcludePaths: excludePaths,
		}, store, newUploadTestSFTPFactory(client).Fn(), logger, nil)
		uploader.client = client

		if err := uploader.Search(filepath.Join(root, "photo")); err != nil {
			t.Fatalf("Search(%#v) error = %v", excludePaths, err)
		}
		if got := store.ReadCalls(); !reflect.DeepEqual(got, []uploadReadCall{{folderPath: filepath.Dir(firstPath), filename: "metadata.yaml"}, {folderPath: filepath.Dir(secondPath), filename: "metadata.yaml"}}) {
			t.Fatalf("read calls with %#v = %#v", excludePaths, got)
		}
		if got := logger.Entries(); !reflect.DeepEqual(got, []string{firstPath + " has already been sent", secondPath + " has already been sent"}) {
			t.Fatalf("logs with %#v = %#v", excludePaths, got)
		}
		if len(store.WriteCalls()) != 0 || len(client.sendCalls) != 0 || len(client.freeSpaceCalls) != 0 || len(client.removeCalls) != 0 {
			t.Fatalf("unexpected side effects with %#v: write=%#v send=%#v free=%#v remove=%#v", excludePaths, store.WriteCalls(), client.sendCalls, client.freeSpaceCalls, client.removeCalls)
		}
	}
}

func TestUploaderSearch_NotSentSuccessWritesSentAndSleeps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int
		wantLog func(string) string
	}{
		{name: "nonzero", size: 17, wantLog: func(path string) string { return fmt.Sprintf("%s: %d", path, 17) }},
		{name: "zero", size: 0, wantLog: func(path string) string { return fmt.Sprintf("same size file %s already exist", path) }},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
			store := newUploadTestMetadataStore()
			store.SetRead(filepath.Dir(targetPath), "metadata.yaml", map[string]protocol.FileMetadata{
				targetPath: {Status: string(protocol.NotSent)},
			})
			client := &uploadTestSFTPClient{
				freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}},
				sendResults:      []uploadSendResult{{size: tt.size}},
			}
			logger := &uploadTestLogger{}
			sleeper := &uploadTestSleeper{}
			uploader := NewUploader(UploadOptions{
				LocalPath:        root,
				SSHPath:          "/remote",
				YAMLFilename:     "metadata.yaml",
				UploadDelay:      3 * time.Second,
				UploadRetryCount: 1,
			}, store, newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
			uploader.client = client

			if err := uploader.Search(root); err != nil {
				t.Fatalf("Search() error = %v", err)
			}

			writes := store.WriteCalls()
			if len(writes) != 1 {
				t.Fatalf("write calls = %#v", writes)
			}
			assertUploadMetadataWrite(t, writes[0], targetPath, "metadata.yaml", 0, protocol.Sent)
			if got := logger.Entries(); !reflect.DeepEqual(got, []string{tt.wantLog(targetPath)}) {
				t.Fatalf("logs = %#v", got)
			}
			if got := sleeper.Durations(); !reflect.DeepEqual(got, []time.Duration{3 * time.Second}) {
				t.Fatalf("sleep durations = %#v", got)
			}
		})
	}
}

func TestUploaderSearch_SendFailureWritesFailedAndSleepsRetries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	destPath := filepath.Join("/remote", strings.TrimPrefix(targetPath, root))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(targetPath), "metadata.yaml", map[string]protocol.FileMetadata{
		targetPath: {Status: string(protocol.NotSent)},
	})
	client := &uploadTestSFTPClient{
		freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}, {size: 1 << 20}},
		sendResults:      []uploadSendResult{{err: stderrors.New("first boom")}, {err: stderrors.New("second boom")}},
	}
	logger := &uploadTestLogger{}
	sleeper := &uploadTestSleeper{}
	uploader := NewUploader(UploadOptions{
		LocalPath:        root,
		SSHPath:          "/remote",
		YAMLFilename:     "metadata.yaml",
		UploadDelay:      5 * time.Second,
		UploadRetryDelay: 2 * time.Second,
		UploadRetryCount: 2,
	}, store, newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
	uploader.client = client

	if err := uploader.Search(root); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	writes := store.WriteCalls()
	if len(writes) != 1 {
		t.Fatalf("write calls = %#v", writes)
	}
	assertUploadMetadataWrite(t, writes[0], targetPath, "metadata.yaml", 0, protocol.Failed)
	if got := client.RemoveCalls(); !reflect.DeepEqual(got, []string{destPath, destPath}) {
		t.Fatalf("remove calls = %#v, want %#v", got, []string{destPath, destPath})
	}
	if got := sleeper.Durations(); !reflect.DeepEqual(got, []time.Duration{2 * time.Second, 2 * time.Second, 5 * time.Second}) {
		t.Fatalf("sleep durations = %#v", got)
	}
	wantLogs := []string{
		fmt.Sprintf("fail to %s send file over sftp: first boom", targetPath),
		"retrying...",
		fmt.Sprintf("fail to %s send file over sftp: second boom", targetPath),
		"retrying...",
		fmt.Sprintf("fail to %s not sent file: fail to %s send file over sftp: second boom", targetPath, targetPath),
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, wantLogs) {
		t.Fatalf("logs = %#v, want %#v", got, wantLogs)
	}
}

func TestUploaderSend_InsufficientSpaceRetriesAndReturnsLastError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("12345678901"))
	client := &uploadTestSFTPClient{freeSpaceResults: []uploadFreeSpaceResult{{size: 5}, {size: 5}}}
	logger := &uploadTestLogger{}
	sleeper := &uploadTestSleeper{}
	uploader := NewUploader(UploadOptions{SpareSpace: 10, UploadRetryDelay: time.Second, UploadRetryCount: 2}, newUploadTestMetadataStore(), newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
	uploader.client = client

	size, err := uploader.Send(targetPath)
	if size != 0 {
		t.Fatalf("Send() size = %d, want 0", size)
	}
	wantErr := "not enough space (\n\ttarget file size: 11\n\tfree space: 5\n\tspare space: 10\n)"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("Send() error = %v", err)
	}
	if len(client.sendCalls) != 0 {
		t.Fatalf("send calls = %#v, want none", client.sendCalls)
	}
	if got := sleeper.Durations(); !reflect.DeepEqual(got, []time.Duration{time.Second, time.Second}) {
		t.Fatalf("sleep durations = %#v", got)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{wantErr, "retrying...", wantErr, "retrying..."}) {
		t.Fatalf("logs = %#v", got)
	}
}

func TestUploaderSend_MissingTargetPathReturnsAndLogsStatError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := filepath.Join(root, "album", "missing.jpg")
	destPath := filepath.Join("/remote", strings.TrimPrefix(targetPath, root))
	client := &uploadTestSFTPClient{
		freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}},
		sendResults:      []uploadSendResult{{size: 13}},
	}
	logger := &uploadTestLogger{}
	uploader := NewUploader(UploadOptions{LocalPath: root, SSHPath: "/remote", UploadRetryCount: 1}, newUploadTestMetadataStore(), newUploadTestSFTPFactory(client).Fn(), logger, nil)
	uploader.client = client

	size, err := uploader.Send(targetPath)
	if size != 13 {
		t.Fatalf("Send() size = %d, want 13", size)
	}
	wantErrPrefix := fmt.Sprintf("fail to get %s file info: ", targetPath)
	if err == nil || !strings.HasPrefix(err.Error(), wantErrPrefix) {
		t.Fatalf("Send() error = %v, want prefix %q", err, wantErrPrefix)
	}
	if strings.Contains(err.Error(), "<nil>") {
		t.Fatalf("Send() error = %q, must not contain <nil>", err.Error())
	}
	if got := logger.Entries(); len(got) != 1 || got[0] != err.Error() {
		t.Fatalf("logs = %#v, want exact returned error %q", got, err.Error())
	}
	if strings.Contains(logger.Entries()[0], "<nil>") {
		t.Fatalf("logs = %#v, must not contain <nil>", logger.Entries())
	}
	if got := client.freeSpaceCalls; !reflect.DeepEqual(got, []string{"/storage/emulated"}) {
		t.Fatalf("free space calls = %#v", got)
	}
	if got := client.sendCalls; !reflect.DeepEqual(got, []uploadSendCall{{localPath: targetPath, remotePath: destPath}}) {
		t.Fatalf("send calls = %#v", got)
	}
	if got := client.RemoveCalls(); len(got) != 0 {
		t.Fatalf("remove calls = %#v, want none", got)
	}
}

func TestUploaderSearch_ZeroRetryCountMarksSentZeroSize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(targetPath), "metadata.yaml", map[string]protocol.FileMetadata{
		targetPath: {Status: string(protocol.NotSent)},
	})
	client := &uploadTestSFTPClient{}
	logger := &uploadTestLogger{}
	sleeper := &uploadTestSleeper{}
	uploader := NewUploader(UploadOptions{YAMLFilename: "metadata.yaml", UploadDelay: 7 * time.Second, UploadRetryCount: 0}, store, newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
	uploader.client = client

	if err := uploader.Search(root); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	writes := store.WriteCalls()
	if len(writes) != 1 {
		t.Fatalf("write calls = %#v", writes)
	}
	assertUploadMetadataWrite(t, writes[0], targetPath, "metadata.yaml", 0, protocol.Sent)
	if len(client.sendCalls) != 0 || len(client.freeSpaceCalls) != 0 {
		t.Fatalf("unexpected sftp calls: send=%#v free=%#v", client.sendCalls, client.freeSpaceCalls)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{fmt.Sprintf("same size file %s already exist", targetPath)}) {
		t.Fatalf("logs = %#v", got)
	}
	if got := sleeper.Durations(); !reflect.DeepEqual(got, []time.Duration{7 * time.Second}) {
		t.Fatalf("sleep durations = %#v", got)
	}
}

func TestUploaderSend_FreeSpaceErrorThenSendSuccessReturnsWrappedLastError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	destPath := filepath.Join("/remote", strings.TrimPrefix(targetPath, root))
	client := &uploadTestSFTPClient{
		freeSpaceResults: []uploadFreeSpaceResult{{err: stderrors.New("statfs failed")}},
		sendResults:      []uploadSendResult{{size: 17}},
	}
	logger := &uploadTestLogger{}
	sleeper := &uploadTestSleeper{}
	uploader := NewUploader(UploadOptions{LocalPath: root, SSHPath: "/remote", UploadRetryCount: 1}, newUploadTestMetadataStore(), newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
	uploader.client = client

	size, err := uploader.Send(targetPath)
	if size != 17 {
		t.Fatalf("Send() size = %d, want 17", size)
	}
	wantErr := "fail to get storage directory information: statfs failed"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("Send() error = %v", err)
	}
	if got := client.sendCalls; !reflect.DeepEqual(got, []uploadSendCall{{localPath: targetPath, remotePath: destPath}}) {
		t.Fatalf("send calls = %#v", got)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{wantErr}) {
		t.Fatalf("logs = %#v", got)
	}
	if got := sleeper.Durations(); len(got) != 0 {
		t.Fatalf("sleep durations = %#v, want none", got)
	}
}

func TestUploaderSend_FirstFailureThenSecondSuccessReturnsFirstError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	destPath := filepath.Join("/remote", strings.TrimPrefix(targetPath, root))
	client := &uploadTestSFTPClient{
		freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}, {size: 1 << 20}},
		sendResults:      []uploadSendResult{{err: stderrors.New("boom")}, {size: 23}},
	}
	logger := &uploadTestLogger{}
	sleeper := &uploadTestSleeper{}
	uploader := NewUploader(UploadOptions{LocalPath: root, SSHPath: "/remote", UploadRetryDelay: 4 * time.Second, UploadRetryCount: 2}, newUploadTestMetadataStore(), newUploadTestSFTPFactory(client).Fn(), logger, sleeper.Sleep)
	uploader.client = client

	size, err := uploader.Send(targetPath)
	if size != 23 {
		t.Fatalf("Send() size = %d, want 23", size)
	}
	wantErr := fmt.Sprintf("fail to %s send file over sftp: boom", targetPath)
	if err == nil || err.Error() != wantErr {
		t.Fatalf("Send() error = %v", err)
	}
	if got := client.RemoveCalls(); !reflect.DeepEqual(got, []string{destPath}) {
		t.Fatalf("remove calls = %#v", got)
	}
	if got := sleeper.Durations(); !reflect.DeepEqual(got, []time.Duration{4 * time.Second}) {
		t.Fatalf("sleep durations = %#v", got)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{wantErr, "retrying..."}) {
		t.Fatalf("logs = %#v", got)
	}
}

func TestUploaderSend_ReconnectTriggerErrorsReuseConnInfoAndPersistOriginalError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sendErr error
		wantErr string
	}{
		{name: "connection lost", sendErr: stderrors.New("connection lost"), wantErr: "connection lost"},
		{name: "no route to host", sendErr: pkgerrors.Wrap(stderrors.New("no route to host"), "wrapped"), wantErr: "wrapped: no route to host"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
			destPath := filepath.Join("/remote", strings.TrimPrefix(targetPath, root))
			oldClient := &uploadTestSFTPClient{
				freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}, {size: 1 << 20}},
				sendResults:      []uploadSendResult{{err: tt.sendErr}, {size: 31}},
			}
			newClient := &uploadTestSFTPClient{
				freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}},
				sendResults:      []uploadSendResult{{size: 29}},
			}
			logger := &uploadTestLogger{}
			sleeper := &uploadTestSleeper{}
			info := &protocol.ConnectionInfo{}
			var factoryCalls int
			var factoryInfos []*protocol.ConnectionInfo
			factory := func(got *protocol.ConnectionInfo) (SFTPClient, error) {
				factoryCalls++
				factoryInfos = append(factoryInfos, got)
				return newClient, nil
			}
			uploader := NewUploader(UploadOptions{LocalPath: root, SSHPath: "/remote", UploadRetryDelay: 2 * time.Second, UploadRetryCount: 2}, newUploadTestMetadataStore(), factory, logger, sleeper.Sleep)
			uploader.client = oldClient
			uploader.connInfo = info

			size, err := uploader.Send(targetPath)
			if size != 29 {
				t.Fatalf("Send() size = %d, want 29", size)
			}
			wantErr := fmt.Sprintf("fail to %s send file over sftp: %s", targetPath, tt.wantErr)
			if err == nil || err.Error() != wantErr {
				t.Fatalf("Send() error = %v", err)
			}
			if factoryCalls != 1 || !reflect.DeepEqual(factoryInfos, []*protocol.ConnectionInfo{info}) {
				t.Fatalf("factory calls/info = %d %#v", factoryCalls, factoryInfos)
			}
			if uploader.client != oldClient {
				t.Fatalf("uploader.client = %p, want old client %p", uploader.client, oldClient)
			}
			if oldClient.closeCalls != 1 {
				t.Fatalf("old client close calls = %d, want 1", oldClient.closeCalls)
			}
			if got := newClient.RemoveCalls(); !reflect.DeepEqual(got, []string{destPath}) {
				t.Fatalf("new client remove calls = %#v", got)
			}
			if got := newClient.sendCalls; !reflect.DeepEqual(got, []uploadSendCall{{localPath: targetPath, remotePath: destPath}}) {
				t.Fatalf("new client send calls = %#v", got)
			}
			if got := oldClient.sendCalls; !reflect.DeepEqual(got, []uploadSendCall{{localPath: targetPath, remotePath: destPath}}) {
				t.Fatalf("old client send calls after reconnect send = %#v", got)
			}

			secondSize, secondErr := uploader.Send(targetPath)
			if secondSize != 31 || secondErr != nil {
				t.Fatalf("second Send() = (%d, %v), want (31, nil)", secondSize, secondErr)
			}
			if got := oldClient.sendCalls; !reflect.DeepEqual(got, []uploadSendCall{{localPath: targetPath, remotePath: destPath}, {localPath: targetPath, remotePath: destPath}}) {
				t.Fatalf("old client send calls = %#v", got)
			}
			if got := newClient.sendCalls; !reflect.DeepEqual(got, []uploadSendCall{{localPath: targetPath, remotePath: destPath}}) {
				t.Fatalf("new client send calls after second send = %#v", got)
			}
			if got := sleeper.Durations(); !reflect.DeepEqual(got, []time.Duration{2 * time.Second}) {
				t.Fatalf("sleep durations = %#v", got)
			}
			if got := logger.Entries(); !reflect.DeepEqual(got, []string{wantErr, "retrying..."}) {
				t.Fatalf("logs = %#v", got)
			}
		})
	}
}

func TestUploaderSearch_ReconnectFactoryErrorPropagatesWithoutTransition(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, "album/photo.jpg", []byte("photo"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(targetPath), "metadata.yaml", map[string]protocol.FileMetadata{
		targetPath: {Status: string(protocol.NotSent)},
	})
	client := &uploadTestSFTPClient{
		freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}},
		sendResults:      []uploadSendResult{{err: stderrors.New("connection lost")}},
	}
	logger := &uploadTestLogger{}
	sleeper := &uploadTestSleeper{}
	dialErr := stderrors.New("dial failed")
	uploader := NewUploader(UploadOptions{
		LocalPath:        root,
		SSHPath:          "/remote",
		YAMLFilename:     "metadata.yaml",
		UploadDelay:      6 * time.Second,
		UploadRetryDelay: 2 * time.Second,
		UploadRetryCount: 2,
	}, store, func(*protocol.ConnectionInfo) (SFTPClient, error) {
		return nil, dialErr
	}, logger, sleeper.Sleep)
	uploader.client = client
	uploader.connInfo = &protocol.ConnectionInfo{}

	err := uploader.Search(root)
	if err == nil {
		t.Fatal("Search() error = nil, want *ReconnectSFTPError")
	}
	var reconnectErr *ReconnectSFTPError
	if !stderrors.As(err, &reconnectErr) {
		t.Fatalf("Search() error = %T %v, want *ReconnectSFTPError", err, err)
	}
	if !stderrors.Is(err, dialErr) && err.Error() != dialErr.Error() {
		t.Fatalf("Search() error = %v, want underlying %v", err, dialErr)
	}
	if got := store.WriteCalls(); len(got) != 0 {
		t.Fatalf("write calls = %#v, want none", got)
	}
	if got := sleeper.Durations(); len(got) != 0 {
		t.Fatalf("sleep durations = %#v, want none", got)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{fmt.Sprintf("fail to %s send file over sftp: connection lost", targetPath)}) {
		t.Fatalf("logs = %#v", got)
	}
	if got := client.RemoveCalls(); len(got) != 0 {
		t.Fatalf("remove calls = %#v, want none", got)
	}
	if client.closeCalls != 0 {
		t.Fatalf("close calls = %d, want 0", client.closeCalls)
	}
}

func TestUploaderSend_DestinationPathUsesCutPrefixAndJoin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, root, filepath.Join("synology", "album", "photo.jpg"), []byte("photo"))
	client := &uploadTestSFTPClient{
		freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}},
		sendResults:      []uploadSendResult{{size: 5}},
	}
	uploader := NewUploader(UploadOptions{LocalPath: root, SSHPath: "/remote/base", UploadRetryCount: 1}, newUploadTestMetadataStore(), newUploadTestSFTPFactory(client).Fn(), &uploadTestLogger{}, nil)
	uploader.client = client

	size, err := uploader.Send(targetPath)
	if size != 5 || err != nil {
		t.Fatalf("Send() = (%d, %v), want (5, nil)", size, err)
	}
	if len(client.sendCalls) != 1 {
		t.Fatalf("send calls = %#v", client.sendCalls)
	}
	wantRemote := filepath.Join("/remote/base", strings.TrimPrefix(targetPath, root))
	if got := client.sendCalls[0].remotePath; got != wantRemote {
		t.Fatalf("remote path = %q, want %q", got, wantRemote)
	}
}

func TestUploaderRun_InitialFactoryErrorReturnsInitialSFTPError(t *testing.T) {
	t.Parallel()

	wantErr := stderrors.New("boom")
	uploader := NewUploader(UploadOptions{}, newUploadTestMetadataStore(), func(*protocol.ConnectionInfo) (SFTPClient, error) {
		return nil, wantErr
	}, &uploadTestLogger{}, nil)

	err := uploader.Run(&protocol.ConnectionInfo{})
	if err == nil {
		t.Fatal("Run() error = nil, want *InitialSFTPError")
	}
	var initialErr *InitialSFTPError
	if !stderrors.As(err, &initialErr) || !stderrors.Is(err, wantErr) {
		t.Fatalf("Run() error = %T %v", err, err)
	}
}

func TestUploaderRun_ReturnsSearchErrorAndLogsCloseError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := &uploadTestSFTPClient{closeErr: stderrors.New("close boom")}
	logger := &uploadTestLogger{}
	uploader := NewUploader(UploadOptions{LocalPath: root, SynologyPath: "missing"}, newUploadTestMetadataStore(), newUploadTestSFTPFactory(client).Fn(), logger, nil)

	err := uploader.Run(&protocol.ConnectionInfo{})
	if err == nil {
		t.Fatal("Run() error = nil, want walk error")
	}
	var pathErr *os.PathError
	if !stderrors.As(err, &pathErr) {
		t.Fatalf("Run() error = %T, want *os.PathError", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("client close calls = %d, want 1", client.closeCalls)
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, []string{"fail to close sftp client: close boom"}) {
		t.Fatalf("logs = %#v", got)
	}
}

func TestUploaderRun_ReconnectClosesInitialClientAndLeavesReplacementUndeferred(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := uploadMustWriteFile(t, filepath.Join(root, "synology"), "album/photo.jpg", []byte("photo"))
	store := newUploadTestMetadataStore()
	store.SetRead(filepath.Dir(targetPath), "metadata.yaml", map[string]protocol.FileMetadata{
		targetPath: {Status: string(protocol.NotSent)},
	})
	initialClient := &uploadTestSFTPClient{freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}}, sendResults: []uploadSendResult{{err: stderrors.New("connection lost")}}}
	currentClient := &uploadTestSFTPClient{freeSpaceResults: []uploadFreeSpaceResult{{size: 1 << 20}}, sendResults: []uploadSendResult{{size: 12}}, closeErr: stderrors.New("current close boom")}
	logger := &uploadTestLogger{}
	uploader := NewUploader(UploadOptions{LocalPath: root, SynologyPath: "synology", SSHPath: "/remote", YAMLFilename: "metadata.yaml", UploadRetryCount: 2}, store, newUploadTestSFTPFactory(initialClient, currentClient).Fn(), logger, nil)

	if err := uploader.Run(&protocol.ConnectionInfo{IP: "1.2.3.4"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if initialClient.closeCalls != 2 {
		t.Fatalf("initial client close calls = %d, want 2", initialClient.closeCalls)
	}
	if currentClient.closeCalls != 0 {
		t.Fatalf("replacement client close calls = %d, want 0", currentClient.closeCalls)
	}
	destPath := filepath.Join("/remote", strings.TrimPrefix(targetPath, root))
	if got := currentClient.sendCalls; !reflect.DeepEqual(got, []uploadSendCall{{localPath: targetPath, remotePath: destPath}}) {
		t.Fatalf("replacement send calls = %#v", got)
	}
	if got := currentClient.RemoveCalls(); !reflect.DeepEqual(got, []string{destPath}) {
		t.Fatalf("replacement remove calls = %#v", got)
	}
	wantLogs := []string{
		fmt.Sprintf("fail to %s send file over sftp: connection lost", targetPath),
		"retrying...",
		fmt.Sprintf("fail to %s not sent file: fail to %s send file over sftp: connection lost", targetPath, targetPath),
	}
	if got := logger.Entries(); !reflect.DeepEqual(got, wantLogs) {
		t.Fatalf("logs = %#v", got)
	}
	if logger.ContainsExact("fail to close sftp client: current close boom") {
		t.Fatalf("logs = %#v", logger.Entries())
	}
}

type uploadReadCall struct {
	folderPath string
	filename   string
}

type uploadMetadataWriteCall struct {
	filePath string
	filename string
	size     uint64
	status   protocol.FileTransferStatus
}

type uploadTestMetadataStore struct {
	reads      map[string]map[string]protocol.FileMetadata
	readErrs   map[string]error
	readCalls  []uploadReadCall
	writeCalls []uploadMetadataWriteCall
	writeErrs  map[string]error
}

func newUploadTestMetadataStore() *uploadTestMetadataStore {
	return &uploadTestMetadataStore{
		reads:     make(map[string]map[string]protocol.FileMetadata),
		readErrs:  make(map[string]error),
		writeErrs: make(map[string]error),
	}
}

func (s *uploadTestMetadataStore) Read(folderPath, filename string) (map[string]protocol.FileMetadata, error) {
	s.readCalls = append(s.readCalls, uploadReadCall{folderPath: folderPath, filename: filename})
	key := filepath.Join(folderPath, filename)
	if err := s.readErrs[key]; err != nil {
		return nil, err
	}
	return uploadCloneMetadataMap(s.reads[key]), nil
}

func (s *uploadTestMetadataStore) Write(filePath, filename string, size uint64, status protocol.FileTransferStatus) error {
	if err := s.writeErrs[filePath]; err != nil {
		return err
	}
	s.writeCalls = append(s.writeCalls, uploadMetadataWriteCall{filePath: filePath, filename: filename, size: size, status: status})
	return nil
}

func (*uploadTestMetadataStore) Exists(string) bool { return false }

func (s *uploadTestMetadataStore) SetRead(folderPath, filename string, metadata map[string]protocol.FileMetadata) {
	s.reads[filepath.Join(folderPath, filename)] = uploadCloneMetadataMap(metadata)
}

func (s *uploadTestMetadataStore) ReadCalls() []uploadReadCall {
	return append([]uploadReadCall(nil), s.readCalls...)
}

func (s *uploadTestMetadataStore) WriteCalls() []uploadMetadataWriteCall {
	return append([]uploadMetadataWriteCall(nil), s.writeCalls...)
}

type uploadSendCall struct {
	localPath  string
	remotePath string
}

type uploadFreeSpaceResult struct {
	size uint64
	err  error
}

type uploadSendResult struct {
	size int
	err  error
}

type uploadTestSFTPClient struct {
	freeSpaceResults []uploadFreeSpaceResult
	sendResults      []uploadSendResult
	removeErr        error
	closeErr         error
	freeSpaceCalls   []string
	sendCalls        []uploadSendCall
	removeCalls      []string
	closeCalls       int
}

func (c *uploadTestSFTPClient) SendFile(localFilePath, remoteFilePath string) (int, error) {
	c.sendCalls = append(c.sendCalls, uploadSendCall{localPath: localFilePath, remotePath: remoteFilePath})
	if len(c.sendResults) == 0 {
		return 0, nil
	}
	result := c.sendResults[0]
	c.sendResults = c.sendResults[1:]
	return result.size, result.err
}

func (c *uploadTestSFTPClient) RemoveFile(targetFilePath string) error {
	c.removeCalls = append(c.removeCalls, targetFilePath)
	return c.removeErr
}

func (c *uploadTestSFTPClient) FreeSpace(path string) (uint64, error) {
	c.freeSpaceCalls = append(c.freeSpaceCalls, path)
	if len(c.freeSpaceResults) == 0 {
		return 0, nil
	}
	result := c.freeSpaceResults[0]
	c.freeSpaceResults = c.freeSpaceResults[1:]
	return result.size, result.err
}

func (c *uploadTestSFTPClient) Close() error {
	c.closeCalls++
	return c.closeErr
}

func (c *uploadTestSFTPClient) RemoveCalls() []string {
	return append([]string(nil), c.removeCalls...)
}

type uploadTestSFTPFactory struct {
	clients []SFTPClient
	errors  []error
	infos   []*protocol.ConnectionInfo
	calls   int
}

func newUploadTestSFTPFactory(clients ...SFTPClient) *uploadTestSFTPFactory {
	return &uploadTestSFTPFactory{clients: clients}
}

func (f *uploadTestSFTPFactory) Fn() SFTPFactory {
	return func(info *protocol.ConnectionInfo) (SFTPClient, error) {
		f.infos = append(f.infos, info)
		idx := f.calls
		f.calls++
		if idx < len(f.errors) && f.errors[idx] != nil {
			return nil, f.errors[idx]
		}
		if len(f.clients) == 0 {
			return nil, nil
		}
		if idx >= len(f.clients) {
			idx = len(f.clients) - 1
		}
		return f.clients[idx], nil
	}
}

func (f *uploadTestSFTPFactory) Infos() []*protocol.ConnectionInfo {
	return append([]*protocol.ConnectionInfo(nil), f.infos...)
}

type uploadTestLogger struct{ entries []string }

func (l *uploadTestLogger) Print(v ...any) { l.entries = append(l.entries, fmt.Sprint(v...)) }
func (l *uploadTestLogger) Printf(format string, v ...any) {
	l.entries = append(l.entries, fmt.Sprintf(format, v...))
}
func (l *uploadTestLogger) Entries() []string { return append([]string(nil), l.entries...) }

func (l *uploadTestLogger) ContainsExact(want string) bool {
	for _, entry := range l.entries {
		if entry == want {
			return true
		}
	}
	return false
}

type uploadTestSleeper struct{ durations []time.Duration }

func (s *uploadTestSleeper) Sleep(d time.Duration) { s.durations = append(s.durations, d) }
func (s *uploadTestSleeper) Durations() []time.Duration {
	return append([]time.Duration(nil), s.durations...)
}

func uploadMustWriteFile(t *testing.T, root, relativePath string, data []byte) string {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func assertUploadMetadataWrite(t *testing.T, got uploadMetadataWriteCall, wantPath, wantFilename string, wantSize uint64, wantStatus protocol.FileTransferStatus) {
	t.Helper()

	if got.filePath != wantPath || got.filename != wantFilename || got.size != wantSize || got.status != wantStatus {
		t.Fatalf("metadata write = %#v, want path=%q filename=%q size=%d status=%q", got, wantPath, wantFilename, wantSize, wantStatus)
	}
}

func uploadCloneMetadataMap(src map[string]protocol.FileMetadata) map[string]protocol.FileMetadata {
	if src == nil {
		return map[string]protocol.FileMetadata{}
	}
	dst := make(map[string]protocol.FileMetadata, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
