package protocol

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewSynologyClientAuthRequest(t *testing.T) {
	var requestErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestErr = checkRequest(r, "/webapi/auth.cgi", url.Values{
			"api":     {"SYNO.API.Auth"},
			"version": {"6"},
			"method":  {"login"},
			"account": {"user name"},
			"passwd":  {"p@ss&word"},
			"session": {"FileStation"},
		})
		_, _ = w.Write([]byte(`{"data":{"sid":"session-id"}}`))
	}))
	defer server.Close()

	info := connectionInfoForServer(t, server, "user name", "p@ss&word")
	client, err := NewSynologyClient(info)
	if err != nil {
		t.Fatalf("NewSynologyClient: %v", err)
	}
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	if client.ConnInfo != info {
		t.Fatal("NewSynologyClient did not preserve the ConnectionInfo pointer")
	}
	if client.SessID != "session-id" {
		t.Fatalf("SessID = %q, want %q", client.SessID, "session-id")
	}
}

func TestNewSynologyClientEmptySIDReturnsConsumedBodyDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":400}}`))
	}))
	defer server.Close()

	_, err := NewSynologyClient(connectionInfoForServer(t, server, "user", "password"))
	if err == nil {
		t.Fatal("NewSynologyClient returned nil error for an empty SID")
	}
	if !strings.Contains(err.Error(), "fail to get new session id: fail to decode") || !strings.Contains(err.Error(), "authentication failed response body: EOF") {
		t.Fatalf("error = %q, want the current consumed-body EOF error", err)
	}
}

func TestSynologyClientGetFileListRequestAndResponse(t *testing.T) {
	var requestErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestErr = checkRequest(r, "/webapi/entry.cgi", url.Values{
			"api":         {"SYNO.FileStation.List"},
			"version":     {"1"},
			"method":      {"list"},
			"folder_path": {"/photos/a & b"},
			"_sid":        {"session-id"},
			"additional":  {"size"},
		})
		_, _ = w.Write([]byte(`{"data":{"files":[{"name":"photo.jpg","path":"/photos/photo.jpg","isdir":false,"additional":{"size":123}}],"offset":2,"total":3},"success":true}`))
	}))
	defer server.Close()

	client := synologyClientForServer(t, server)
	response, err := client.GetFileList("/photos/a & b")
	if err != nil {
		t.Fatalf("GetFileList: %v", err)
	}
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	if !response.Success || response.Data.Offset != 2 || response.Data.Total != 3 || len(response.Data.Files) != 1 {
		t.Fatalf("response = %#v, want parsed list metadata", response)
	}
	file := response.Data.Files[0]
	if file.Name != "photo.jpg" || file.Path != "/photos/photo.jpg" || file.IsDir || file.Additional.Size != 123 {
		t.Fatalf("file = %#v, want parsed file fields", file)
	}
}

func TestSynologyClientGetFileListFalseSuccessReturnsResponseWithoutError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"offset":4,"total":9},"success":false}`))
	}))
	defer server.Close()

	response, err := synologyClientForServer(t, server).GetFileList("/photos")
	if err != nil {
		t.Fatalf("GetFileList returned error for success=false: %v", err)
	}
	if response == nil || response.Success || response.Data.Offset != 4 || response.Data.Total != 9 {
		t.Fatalf("response = %#v, want parsed success=false response", response)
	}
}

func TestSynologyClientDownloadRequestAndFalseSuccessBody(t *testing.T) {
	body := []byte(`{"success":false,"error":{"code":404}}`)
	var requestErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestErr = checkRequest(r, "/webapi/entry.cgi", url.Values{
			"api":     {"SYNO.FileStation.Download"},
			"version": {"1"},
			"method":  {"download"},
			"path":    {"/photos/a & b.jpg"},
			"_sid":    {"session-id"},
		})
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "downloaded.jpg")
	gotPath, gotSize, err := synologyClientForServer(t, server).DownloadFile("/photos/a & b.jpg", destPath)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	if gotPath != destPath || gotSize != int64(len(body)) {
		t.Fatalf("DownloadFile = (%q, %d), want (%q, %d)", gotPath, gotSize, destPath, len(body))
	}
	gotBody, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if !reflect.DeepEqual(gotBody, body) {
		t.Fatalf("destination body = %q, want %q", gotBody, body)
	}
	if FileExists(destPath + ".download") {
		t.Fatalf("temporary file %q still exists after download", destPath+".download")
	}
}

func TestSynologyClientDownloadUsesTemporaryFileBeforeRename(t *testing.T) {
	firstChunkWritten := make(chan struct{})
	finishResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("first"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(firstChunkWritten)
		<-finishResponse
		_, _ = w.Write([]byte("second"))
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "downloaded.jpg")
	type result struct {
		path string
		size int64
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		path, size, err := synologyClientForServer(t, server).DownloadFile("/photos/file.jpg", destPath)
		resultCh <- result{path: path, size: size, err: err}
	}()

	<-firstChunkWritten
	tempPath := destPath + ".download"
	deadline := time.Now().Add(2 * time.Second)
	for !FileExists(tempPath) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !FileExists(tempPath) {
		close(finishResponse)
		t.Fatalf("temporary file %q was not created while the response was in progress", tempPath)
	}
	if FileExists(destPath) {
		close(finishResponse)
		t.Fatalf("destination %q existed before the response copy completed", destPath)
	}

	close(finishResponse)
	got := <-resultCh
	if got.err != nil {
		t.Fatalf("DownloadFile: %v", got.err)
	}
	if got.path != destPath || got.size != int64(len("firstsecond")) {
		t.Fatalf("DownloadFile = (%q, %d), want (%q, %d)", got.path, got.size, destPath, len("firstsecond"))
	}
	if FileExists(tempPath) || !FileExists(destPath) {
		t.Fatalf("after download: temporary exists=%v, destination exists=%v", FileExists(tempPath), FileExists(destPath))
	}
}

func checkRequest(r *http.Request, wantPath string, wantQuery url.Values) error {
	if r.Method != http.MethodGet {
		return fmt.Errorf("method = %q, want GET", r.Method)
	}
	if r.URL.Path != wantPath {
		return fmt.Errorf("path = %q, want %q", r.URL.Path, wantPath)
	}
	if got := r.URL.Query(); !reflect.DeepEqual(got, wantQuery) {
		return fmt.Errorf("query = %#v, want %#v", got, wantQuery)
	}
	return nil
}

func connectionInfoForServer(t *testing.T, server *httptest.Server, username, password string) *ConnectionInfo {
	t.Helper()
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return &ConnectionInfo{IP: host, Port: port, Username: username, Password: password}
}

func synologyClientForServer(t *testing.T, server *httptest.Server) *SynologyClient {
	t.Helper()
	return &SynologyClient{
		ConnInfo: connectionInfoForServer(t, server, "user", "password"),
		SessID:   "session-id",
	}
}
