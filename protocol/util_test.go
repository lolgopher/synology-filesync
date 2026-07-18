package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSameFileSize(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target")
	equalPath := filepath.Join(dir, "equal")
	differentPath := filepath.Join(dir, "different")
	for path, contents := range map[string]string{
		targetPath:    "1234",
		equalPath:     "abcd",
		differentPath: "12345",
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	equalInfo, err := os.Stat(equalPath)
	if err != nil {
		t.Fatalf("stat equal file: %v", err)
	}
	same, err := IsSameFileSize(targetPath, equalInfo)
	if err != nil || !same {
		t.Fatalf("IsSameFileSize(equal) = (%v, %v), want (true, nil)", same, err)
	}

	differentInfo, err := os.Stat(differentPath)
	if err != nil {
		t.Fatalf("stat different file: %v", err)
	}
	same, err = IsSameFileSize(targetPath, differentInfo)
	if err != nil || same {
		t.Fatalf("IsSameFileSize(different) = (%v, %v), want (false, nil)", same, err)
	}
}

func TestIsSameFileSizeMissingTargetReturnsWrappedStatError(t *testing.T) {
	dir := t.TempDir()
	comparePath := filepath.Join(dir, "compare")
	if err := os.WriteFile(comparePath, []byte("data"), 0600); err != nil {
		t.Fatalf("write compare file: %v", err)
	}
	compareInfo, err := os.Stat(comparePath)
	if err != nil {
		t.Fatalf("stat compare file: %v", err)
	}

	missingPath := filepath.Join(dir, "missing")
	same, err := IsSameFileSize(missingPath, compareInfo)
	if err == nil || same {
		t.Fatalf("IsSameFileSize(missing) = (%v, %v), want (false, error)", same, err)
	}
	if !strings.Contains(err.Error(), "fail to get stat "+missingPath+" file:") {
		t.Fatalf("error = %q, want wrapped stat error for %q", err, missingPath)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "file")
	if err := os.WriteFile(filePath, []byte("data"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	for name, test := range map[string]struct {
		path string
		want bool
	}{
		"file":      {path: filePath, want: true},
		"directory": {path: dir, want: true},
		"missing":   {path: filepath.Join(dir, "missing"), want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := FileExists(test.path); got != test.want {
				t.Fatalf("FileExists(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestPublicUtilityCompatibility(t *testing.T) {
	var _ = ConnectionInfo{IP: "host", Port: 22, Username: "user", Password: "password"}
	var _ func(string, os.FileInfo) (bool, error) = IsSameFileSize
	var _ func(string) bool = FileExists
}
