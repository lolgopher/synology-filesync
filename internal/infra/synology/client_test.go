package synology

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/lolgopher/synology-filesync/protocol"
)

func TestClientDelegatesAuthenticationListAndDownload(t *testing.T) {
	const (
		username = "user@example.com"
		password = "secret value"
		session  = "session-id"
		folder   = "/shared folder"
		filePath = "/shared folder/file.txt"
		contents = "downloaded contents"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webapi/auth.cgi":
			assertQuery(t, r.URL.Query(), map[string]string{
				"api": "SYNO.API.Auth", "version": "6", "method": "login",
				"account": username, "passwd": password, "session": "FileStation",
			})
			_, _ = fmt.Fprintf(w, `{"data":{"sid":%q}}`, session)
		case "/webapi/entry.cgi":
			switch r.URL.Query().Get("api") {
			case "SYNO.FileStation.List":
				assertQuery(t, r.URL.Query(), map[string]string{
					"api": "SYNO.FileStation.List", "version": "1", "method": "list",
					"folder_path": folder, "_sid": session, "additional": "size",
				})
				_, _ = w.Write([]byte(`{"data":{"files":[{"name":"file.txt","path":"/shared folder/file.txt","isdir":false,"additional":{"size":19}}],"offset":0,"total":1},"success":true}`))
			case "SYNO.FileStation.Download":
				assertQuery(t, r.URL.Query(), map[string]string{
					"api": "SYNO.FileStation.Download", "version": "1", "method": "download",
					"path": filePath, "_sid": session,
				})
				_, _ = w.Write([]byte(contents))
			default:
				http.Error(w, "unexpected API", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(connectionInfo(t, server.URL, username, password))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	list, err := client.GetFileList(folder)
	if err != nil {
		t.Fatalf("GetFileList() error = %v", err)
	}
	if !list.Success || list.Data.Total != 1 || len(list.Data.Files) != 1 || list.Data.Files[0].Path != filePath {
		t.Fatalf("GetFileList() = %#v", list)
	}

	destination := filepath.Join(t.TempDir(), "file.txt")
	gotPath, gotSize, err := client.DownloadFile(filePath, destination)
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	if gotPath != destination || gotSize != int64(len(contents)) {
		t.Fatalf("DownloadFile() = (%q, %d), want (%q, %d)", gotPath, gotSize, destination, len(contents))
	}
	gotContents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(gotContents) != contents {
		t.Fatalf("downloaded contents = %q, want %q", gotContents, contents)
	}
}

func TestClientPassesThroughProtocolErrors(t *testing.T) {
	t.Run("authentication", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer server.Close()
		info := connectionInfo(t, server.URL, "user", "password")

		_, protocolErr := protocol.NewSynologyClient(info)
		_, adapterErr := NewClient(info)
		assertSameError(t, adapterErr, protocolErr)
	})

	t.Run("list", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer server.Close()
		protocolClient := &protocol.SynologyClient{ConnInfo: connectionInfo(t, server.URL, "", ""), SessID: "sid"}
		adapter := &Client{client: protocolClient}

		_, protocolErr := protocolClient.GetFileList("/folder")
		_, adapterErr := adapter.GetFileList("/folder")
		assertSameError(t, adapterErr, protocolErr)
	})

	t.Run("download", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("contents"))
		}))
		defer server.Close()
		protocolClient := &protocol.SynologyClient{ConnInfo: connectionInfo(t, server.URL, "", ""), SessID: "sid"}
		adapter := &Client{client: protocolClient}
		missingDirectory := filepath.Join(t.TempDir(), "missing", "file.txt")

		_, _, protocolErr := protocolClient.DownloadFile("/file.txt", missingDirectory)
		_, _, adapterErr := adapter.DownloadFile("/file.txt", missingDirectory)
		assertSameError(t, adapterErr, protocolErr)
	})
}

func connectionInfo(t *testing.T, serverURL, username, password string) *protocol.ConnectionInfo {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("strconv.Atoi() error = %v", err)
	}
	return &protocol.ConnectionInfo{IP: host, Port: port, Username: username, Password: password}
}

func assertQuery(t *testing.T, got url.Values, want map[string]string) {
	t.Helper()
	for key, value := range want {
		if got.Get(key) != value {
			t.Errorf("query %q = %q, want %q", key, got.Get(key), value)
		}
	}
}

func assertSameError(t *testing.T, got, want error) {
	t.Helper()
	if got == nil || want == nil {
		t.Fatalf("errors = (%v, %v), want both non-nil", got, want)
	}
	if got.Error() != want.Error() {
		t.Fatalf("adapter error = %q, protocol error = %q", got, want)
	}
}
