package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

const mainVersionHelperEnv = "SYNology_FILESYNC_MAIN_VERSION_HELPER"

func TestMainVersion(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainVersionHelper$")
	cmd.Env = append(os.Environ(), mainVersionHelperEnv+"=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version subprocess failed: %v\noutput:\n%s", err, output)
	}

	timestamp := regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	want := []string{
		"config: ",
		"v: true",
		"synology-filesync-unknown-unknown(unknown)",
	}
	if len(lines) != len(want) {
		t.Fatalf("version output lines = %q, want %q", lines, want)
	}
	for i := range lines {
		if !timestamp.MatchString(lines[i]) {
			t.Errorf("version output line %d = %q, want timestamp prefix", i+1, lines[i])
			continue
		}
		got := timestamp.ReplaceAllString(lines[i], "")
		if got != want[i] {
			t.Errorf("version output line %d = %q, want %q", i+1, got, want[i])
		}
	}
}

func TestMainVersionHelper(t *testing.T) {
	if os.Getenv(mainVersionHelperEnv) != "1" {
		return
	}

	os.Args = []string{os.Args[0], "-v"}
	main()
	t.Fatal("main returned after -v; want process exit")
}
