package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/lolgopher/synology-filesync/protocol"
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
