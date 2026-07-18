package tool

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	metadata "github.com/lolgopher/synology-filesync/protocol"
)

func TestGetStatusFiltersMetadataInNestedDirectories(t *testing.T) {
	tests := []struct {
		name   string
		status metadata.FileTransferStatus
		get    func(string) (map[string]metadata.FileMetadata, error)
	}{
		{name: "init", status: metadata.Init, get: GetInitStatus},
		{name: "not sent", status: metadata.NotSent, get: GetNotSentStatus},
		{name: "sent", status: metadata.Sent, get: GetSentStatus},
		{name: "failed", status: metadata.Failed, get: GetFailedStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			nested := filepath.Join(root, "nested")
			rootMatch := filepath.Join(root, tt.name+"-root.jpg")
			nestedMatch := filepath.Join(nested, tt.name+"-nested.jpg")
			other := filepath.Join(root, tt.name+"-other.jpg")

			writeMetadata(t, root, "metadata.yaml", map[string]metadata.FileTransferStatus{
				rootMatch: tt.status,
				other:     differentStatus(tt.status),
			})
			writeMetadata(t, nested, "metadata.yaml", map[string]metadata.FileTransferStatus{
				nestedMatch: tt.status,
			})

			got, err := tt.get(root)
			if err != nil {
				t.Fatalf("scan metadata: %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("got %d matching entries, want 2: %#v", len(got), got)
			}
			for _, path := range []string{rootMatch, nestedMatch} {
				entry, ok := got[path]
				if !ok {
					t.Errorf("missing complete path key %q", path)
					continue
				}
				if entry.Status != string(tt.status) {
					t.Errorf("status for %q = %q, want %q", path, entry.Status, tt.status)
				}
			}
			if _, ok := got[other]; ok {
				t.Errorf("included entry with a different status: %q", other)
			}
		})
	}
}

func TestGetSentStatusUsesDefaultMetadataFilenameAndIgnoresMissingFiles(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	ignoredPath := filepath.Join(root, "ignored.jpg")
	wantPath := filepath.Join(nested, "sent.jpg")

	writeMetadata(t, root, "other.yaml", map[string]metadata.FileTransferStatus{
		ignoredPath: metadata.Sent,
	})
	writeMetadata(t, nested, "metadata.yaml", map[string]metadata.FileTransferStatus{
		wantPath: metadata.Sent,
	})

	got, err := GetSentStatus(root)
	if err != nil {
		t.Fatalf("scan metadata: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %#v", len(got), got)
	}
	if _, ok := got[wantPath]; !ok {
		t.Errorf("missing entry from metadata.yaml: %q", wantPath)
	}
	if _, ok := got[ignoredPath]; ok {
		t.Errorf("included entry from non-default metadata filename: %q", ignoredPath)
	}
}

func TestGetSentStatusMissingRootReturnsPathError(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing")

	got, err := GetSentStatus(missingRoot)
	if err == nil {
		t.Fatal("scan missing root returned nil error")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("scan missing root error = %T, want *os.PathError: %v", err, err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scan missing root error = %v, want os.ErrNotExist", err)
	}
	if pathErr.Path != missingRoot {
		t.Fatalf("scan missing root path = %q, want %q", pathErr.Path, missingRoot)
	}
	if len(got) != 0 {
		t.Fatalf("scan missing root returned %d entries, want 0: %#v", len(got), got)
	}
}

func writeMetadata(t *testing.T, folderPath, filename string, entries map[string]metadata.FileTransferStatus) {
	t.Helper()

	if err := os.MkdirAll(folderPath, 0o755); err != nil {
		t.Fatalf("create metadata directory: %v", err)
	}
	type metadataEntry struct {
		Status metadata.FileTransferStatus `json:"status"`
	}
	data := make(map[string]metadataEntry, len(entries))
	for path, status := range entries {
		data[path] = metadataEntry{Status: status}
	}
	contents, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folderPath, filename), contents, 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

func differentStatus(status metadata.FileTransferStatus) metadata.FileTransferStatus {
	if status == metadata.Failed {
		return metadata.Sent
	}
	return metadata.Failed
}
