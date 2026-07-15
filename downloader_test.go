package main

import (
	"bytes"
	"log"
	"testing"
)

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
