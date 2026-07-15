package metadata

import (
	"path/filepath"
	"testing"

	"github.com/lolgopher/synology-filesync/internal/app"
	"github.com/lolgopher/synology-filesync/protocol"
)

var _ app.MetadataStore = (*Store)(nil)

func TestStoreWrite(t *testing.T) {
	store := NewStore()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "photo.jpg")
	metadataName := "metadata.yaml"

	if err := store.Write(filePath, metadataName, 7, protocol.Init); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	metadata, err := protocol.ReadMetadata(dir, metadataName)
	if err != nil {
		t.Fatalf("ReadMetadata() error = %v", err)
	}

	want := map[string]protocol.FileMetadata{
		filePath: {
			Size:   7,
			Status: protocol.Init,
		},
	}
	if len(metadata) != len(want) {
		t.Fatalf("metadata entry count = %d, want %d", len(metadata), len(want))
	}
	if got := metadata[filePath]; got != want[filePath] {
		t.Fatalf("metadata[%q] = %#v, want %#v", filePath, got, want[filePath])
	}
}

func TestStoreRead(t *testing.T) {
	store := NewStore()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "video.mp4")
	metadataName := "metadata.yaml"

	if err := protocol.WriteMetadata(filePath, metadataName, 11, protocol.Init); err != nil {
		t.Fatalf("WriteMetadata(init) error = %v", err)
	}
	if err := protocol.WriteMetadata(filePath, metadataName, 0, protocol.Sent); err != nil {
		t.Fatalf("WriteMetadata(sent) error = %v", err)
	}

	metadata, err := store.Read(dir, metadataName)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	want := map[string]protocol.FileMetadata{
		filePath: {
			Size:   11,
			Status: protocol.Sent,
		},
	}
	if len(metadata) != len(want) {
		t.Fatalf("metadata entry count = %d, want %d", len(metadata), len(want))
	}
	if got := metadata[filePath]; got != want[filePath] {
		t.Fatalf("metadata[%q] = %#v, want %#v", filePath, got, want[filePath])
	}
}

func TestStoreExists(t *testing.T) {
	store := NewStore()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "archive.zip")
	metadataName := "metadata.yaml"
	metadataPath := filepath.Join(dir, metadataName)
	missingPath := filepath.Join(dir, "missing.yaml")

	if err := protocol.WriteMetadata(filePath, metadataName, 5, protocol.Init); err != nil {
		t.Fatalf("WriteMetadata() error = %v", err)
	}

	if got, want := store.Exists(metadataPath), protocol.FileExists(metadataPath); got != want {
		t.Fatalf("Exists(%q) = %v, want %v", metadataPath, got, want)
	}
	if got, want := store.Exists(missingPath), protocol.FileExists(missingPath); got != want {
		t.Fatalf("Exists(%q) = %v, want %v", missingPath, got, want)
	}
	if store.Exists(missingPath) {
		t.Fatalf("Exists(%q) = true, want false", missingPath)
	}
}
