package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestWriteMetadata_YAMLShapeAndStatuses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metadataName := "metadata.yaml"
	filePath := filepath.Join(dir, "nested", "file.txt")
	metadataPath := filepath.Join(filepath.Dir(filePath), metadataName)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	statuses := []FileTransferStatus{Init, NotSent, Sent, Failed, FileTransferStatus("SOMETHING_ELSE")}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			if err := WriteMetadata(filePath, metadataName, 123, Init); err != nil {
				t.Fatalf("seed init metadata: %v", err)
			}
			if err := WriteMetadata(filePath, metadataName, 999, status); err != nil {
				t.Fatalf("write metadata: %v", err)
			}

			raw, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatalf("read raw metadata: %v", err)
			}

			var decoded map[string]FileMetadata
			if err := yaml.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal raw metadata: %v", err)
			}

			entry, ok := decoded[filePath]
			if !ok {
				t.Fatalf("expected exact filePath key %q in %#v", filePath, decoded)
			}
			wantSize := 123
			if status == Init {
				wantSize = 999
			}
			if entry.Size != uint64(wantSize) {
				t.Fatalf("size = %d, want %d", entry.Size, wantSize)
			}
			if entry.Status != status {
				t.Fatalf("status = %q, want %q", entry.Status, status)
			}
			if !strings.Contains(string(raw), "status: "+string(status)+"\n") {
				t.Fatalf("status is not a plain scalar in YAML:\n%s", raw)
			}

			metadata, err := ReadMetadata(filepath.Dir(filePath), metadataName)
			if err != nil {
				t.Fatalf("read metadata: %v", err)
			}
			if got := metadata[filePath].Size; got != uint64(wantSize) {
				t.Fatalf("read size = %d, want %d", got, wantSize)
			}
			if got := metadata[filePath].Status; got != status {
				t.Fatalf("read status = %q, want %q", got, status)
			}
		})
	}
}

func TestWriteMetadata_InitUsesSuppliedSizeAndNonInitPreservesExistingSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metadataName := "metadata.yaml"
	filePath := filepath.Join(dir, "file.txt")

	if err := WriteMetadata(filePath, metadataName, 41, Init); err != nil {
		t.Fatalf("write init metadata: %v", err)
	}
	if err := WriteMetadata(filePath, metadataName, 99, Sent); err != nil {
		t.Fatalf("write sent metadata: %v", err)
	}

	metadata, err := ReadMetadata(dir, metadataName)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	if got := metadata[filePath]; got.Size != 41 || got.Status != Sent {
		t.Fatalf("metadata[%q] = %#v, want size preserved at 41 and status %q", filePath, got, Sent)
	}
}

func TestWriteMetadataAndReadMetadata_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metadataName := "metadata.yaml"
	filePath := filepath.Join(dir, "roundtrip.txt")

	want := map[string]FileMetadata{
		filePath: {Size: 7, Status: Init},
	}

	if err := WriteMetadata(filePath, metadataName, want[filePath].Size, Init); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	got, err := ReadMetadata(dir, metadataName)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if len(got) != len(want) || got[filePath] != want[filePath] {
		t.Fatalf("round trip metadata = %#v, want %#v", got, want)
	}
}

func TestWriteMetadata_NonInitWithoutExistingEntryUsesZeroSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metadataName := "metadata.yaml"
	filePath := filepath.Join(dir, "missing-init.txt")

	if err := WriteMetadata(filePath, metadataName, 88, Sent); err != nil {
		t.Fatalf("write non-init metadata: %v", err)
	}

	metadata, err := ReadMetadata(dir, metadataName)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	if got := metadata[filePath]; got.Size != 0 || got.Status != Sent {
		t.Fatalf("metadata[%q] = %#v, want zero size and status %q", filePath, got, Sent)
	}
}

func TestReadMetadata_MissingFileError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := ReadMetadata(dir, "missing.yaml")
	if err == nil {
		t.Fatal("expected missing-file error")
	}
	if !strings.Contains(err.Error(), "fail to read") {
		t.Fatalf("error = %q, want read failure", err)
	}
}

func TestWriteMetadata_MissingFileCreatesMetadataFileWithEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metadataName := "metadata.yaml"
	filePath := filepath.Join(dir, "created.txt")

	if err := WriteMetadata(filePath, metadataName, 55, Init); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	metadataPath := filepath.Join(dir, metadataName)
	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("stat metadata file: %v", err)
	}

	metadata, err := ReadMetadata(dir, metadataName)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if got := metadata[filePath]; got.Size != 55 || got.Status != Init {
		t.Fatalf("metadata[%q] = %#v, want size 55 and status %q", filePath, got, Init)
	}
}

func TestReadMetadata_MalformedYAMLError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "metadata.yaml")
	if err := os.WriteFile(metadataPath, []byte("broken: [\n"), 0644); err != nil {
		t.Fatalf("seed malformed metadata: %v", err)
	}

	_, err := ReadMetadata(dir, filepath.Base(metadataPath))
	if err == nil {
		t.Fatal("expected malformed-yaml error")
	}
	if !strings.Contains(err.Error(), "fail to unmarshal") {
		t.Fatalf("error = %q, want unmarshal failure", err)
	}
}

func TestWriteMetadata_MalformedYAMLError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metadataName := "metadata.yaml"
	if err := os.WriteFile(filepath.Join(dir, metadataName), []byte("broken: [\n"), 0644); err != nil {
		t.Fatalf("seed malformed metadata: %v", err)
	}

	err := WriteMetadata(filepath.Join(dir, "file.txt"), metadataName, 12, Init)
	if err == nil {
		t.Fatal("expected malformed-yaml error")
	}
	if !strings.Contains(err.Error(), "fail to unmarshal") {
		t.Fatalf("error = %q, want unmarshal failure", err)
	}
}
