package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/lolgopher/synology-filesync/protocol"
)

func TestSearchLocalSkipsLiteralMetadataYAMLWhenConfiguredFilenameDiffers(t *testing.T) {
	configureMetadataFilename(t, "transfer-state.yaml")

	root := t.TempDir()
	metadataPath := filepath.Join(root, "metadata.yaml")
	if err := os.WriteFile(metadataPath, []byte("not valid metadata: [\n"), 0o600); err != nil {
		t.Fatalf("write literal metadata.yaml: %v", err)
	}

	if err := searchLocal(nil, root); err != nil {
		t.Fatalf("search local: %v", err)
	}
	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("literal metadata.yaml changed or removed: %v", err)
	}
}

func TestSearchLocalUnknownStatusUsesDefaultHandling(t *testing.T) {
	metadataFilename := configureMetadataFilename(t, "metadata.yaml")

	root := t.TempDir()
	filePath := filepath.Join(root, "payload.bin")
	if err := os.WriteFile(filePath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	unknown := protocol.FileTransferStatus("SOMETHING_ELSE")
	if err := protocol.WriteMetadata(filePath, metadataFilename, 7, unknown); err != nil {
		t.Fatalf("write unknown metadata status: %v", err)
	}

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

	if err := searchLocal(nil, root); err != nil {
		t.Fatalf("search local: %v", err)
	}
	if want := string(unknown) + " is unknown status\n"; logs.String() != want {
		t.Fatalf("logs = %q, want %q", logs.String(), want)
	}
	metadata, err := protocol.ReadMetadata(root, metadataFilename)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if got := metadata[filePath].Status; got != unknown {
		t.Fatalf("status = %q, want %q", got, unknown)
	}
}
