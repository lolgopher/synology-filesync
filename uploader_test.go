package main

import (
	"bytes"
	"errors"
	"fmt"
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
	wantContent := []byte("not valid metadata: [\n")
	if err := os.WriteFile(metadataPath, wantContent, 0o600); err != nil {
		t.Fatalf("write literal metadata.yaml: %v", err)
	}

	if err := searchLocal(nil, root); err != nil {
		t.Fatalf("search local: %v", err)
	}
	gotContent, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read literal metadata.yaml: %v", err)
	}
	if !bytes.Equal(gotContent, wantContent) {
		t.Fatalf("literal metadata.yaml content = %q, want %q", gotContent, wantContent)
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

func TestSearchLocalSkipsKnownNonTransferStatusesWithoutSFTP(t *testing.T) {
	metadataFilename := configureMetadataFilename(t, "metadata.yaml")

	tests := []struct {
		name    string
		status  protocol.FileTransferStatus
		wantLog string
	}{
		{name: "init", status: protocol.Init, wantLog: "%s is init metadata status\n"},
		{name: "sent", status: protocol.Sent, wantLog: "%s has already been sent\n"},
		{name: "failed", status: protocol.Failed, wantLog: "%s sent failed\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			filePath := filepath.Join(root, "payload.bin")
			if err := os.WriteFile(filePath, []byte("payload"), 0o600); err != nil {
				t.Fatalf("write payload: %v", err)
			}
			if err := protocol.WriteMetadata(filePath, metadataFilename, 7, tt.status); err != nil {
				t.Fatalf("write metadata: %v", err)
			}
			beforeMetadata, err := protocol.ReadMetadata(root, metadataFilename)
			if err != nil {
				t.Fatalf("read seeded metadata: %v", err)
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
			if want := fmt.Sprintf(tt.wantLog, filePath); logs.String() != want {
				t.Fatalf("logs = %q, want %q", logs.String(), want)
			}
			afterMetadata, err := protocol.ReadMetadata(root, metadataFilename)
			if err != nil {
				t.Fatalf("read resulting metadata: %v", err)
			}
			if beforeMetadata[filePath] != afterMetadata[filePath] {
				t.Fatalf("metadata[%q] changed: got %#v want %#v", filePath, afterMetadata[filePath], beforeMetadata[filePath])
			}
		})
	}
}

func TestSearchLocalMissingEntryReturnsExactErrorWithoutSFTP(t *testing.T) {
	metadataFilename := configureMetadataFilename(t, "metadata.yaml")
	root := t.TempDir()
	filePath := filepath.Join(root, "payload.bin")
	if err := os.WriteFile(filePath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	otherPath := filepath.Join(root, "other.bin")
	if err := protocol.WriteMetadata(otherPath, metadataFilename, 5, protocol.NotSent); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	err := searchLocal(nil, root)
	if err == nil {
		t.Fatal("searchLocal returned nil error for a file missing from metadata")
	}
	if want := "fail to find " + filePath + " in metadata"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestSearchLocalMissingRootReturnsWalkErrorWithoutSFTP(t *testing.T) {
	configureMetadataFilename(t, "metadata.yaml")
	missingRoot := filepath.Join(t.TempDir(), "missing-root")

	err := searchLocal(nil, missingRoot)
	if err == nil {
		t.Fatal("searchLocal returned nil error for a missing root")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want os.ErrNotExist", err)
	}
	if got := err.Error(); got == "" || !bytes.Contains([]byte(got), []byte(missingRoot)) {
		t.Fatalf("error = %q, want the missing root path in the walk error", got)
	}
}
