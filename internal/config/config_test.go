package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func newValidConfig() *Config {
	return &Config{
		DownloadType: "synology",
		Synology: &Address{
			IP:       "1.2.3.4",
			Port:     5001,
			Username: "admin",
			Password: "pass",
			Path:     "/photo",
		},
		UploadType: "ssh",
		SSH: &Address{
			IP:       "192.168.0.100",
			Port:     22,
			Username: "user",
			Password: "pass",
			Path:     "/DCIM",
		},
		DBType: "yaml",
		YAML: &FileDB{
			Filename: "metadata.yaml",
		},
		LocalPath:      "/tmp/synology-filesync",
		DownloadWorker: 1,
	}
}

func TestVerifyConfigValid(t *testing.T) {
	if err := verifyConfig(newValidConfig()); err != nil {
		t.Fatalf("verifyConfig() error = %v, want nil", err)
	}
}

func TestValidateNil(t *testing.T) {
	err := Validate(nil)
	if err == nil {
		t.Fatal("Validate(nil) error = nil, want config-required error")
	}
	if err.Error() != "config is required" {
		t.Fatalf("Validate(nil) error = %q, want %q", err.Error(), "config is required")
	}
}

func TestVerifyConfigExactErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "synology ip",
			mutate: func(cfg *Config) {
				cfg.Synology.IP = ""
			},
			wantErr: "synology ip address is required",
		},
		{
			name: "synology nil",
			mutate: func(cfg *Config) {
				cfg.Synology = nil
			},
			wantErr: "synology ip address is required",
		},
		{
			name: "synology port required",
			mutate: func(cfg *Config) {
				cfg.Synology.Port = 0
			},
			wantErr: "synology port is required",
		},
		{
			name: "synology port invalid",
			mutate: func(cfg *Config) {
				cfg.Synology.Port = -1
			},
			wantErr: "invalid synology port number",
		},
		{
			name: "synology username",
			mutate: func(cfg *Config) {
				cfg.Synology.Username = ""
			},
			wantErr: "synology username is required",
		},
		{
			name: "synology password",
			mutate: func(cfg *Config) {
				cfg.Synology.Password = ""
			},
			wantErr: "synology password is required",
		},
		{
			name: "synology path",
			mutate: func(cfg *Config) {
				cfg.Synology.Path = ""
			},
			wantErr: "filestation path is required",
		},
		{
			name: "download worker zero",
			mutate: func(cfg *Config) {
				cfg.DownloadWorker = 0
			},
			wantErr: "download worker must be positive",
		},
		{
			name: "download worker negative",
			mutate: func(cfg *Config) {
				cfg.DownloadWorker = -1
			},
			wantErr: "download worker must be positive",
		},
		{
			name: "ssh ip",
			mutate: func(cfg *Config) {
				cfg.SSH.IP = ""
			},
			wantErr: "ssh ip address is required",
		},
		{
			name: "ssh nil",
			mutate: func(cfg *Config) {
				cfg.SSH = nil
			},
			wantErr: "ssh ip address is required",
		},
		{
			name: "ssh port required",
			mutate: func(cfg *Config) {
				cfg.SSH.Port = 0
			},
			wantErr: "ssh port is required",
		},
		{
			name: "ssh port invalid",
			mutate: func(cfg *Config) {
				cfg.SSH.Port = -1
			},
			wantErr: "invalid ssh port number",
		},
		{
			name: "ssh username",
			mutate: func(cfg *Config) {
				cfg.SSH.Username = ""
			},
			wantErr: "ssh username is required",
		},
		{
			name: "ssh password",
			mutate: func(cfg *Config) {
				cfg.SSH.Password = ""
			},
			wantErr: "ssh password is required",
		},
		{
			name: "ssh path",
			mutate: func(cfg *Config) {
				cfg.SSH.Path = ""
			},
			wantErr: "ssh path is required",
		},
		{
			name: "yaml filename",
			mutate: func(cfg *Config) {
				cfg.YAML.Filename = ""
			},
			wantErr: "filename is required",
		},
		{
			name: "yaml nil",
			mutate: func(cfg *Config) {
				cfg.YAML = nil
			},
			wantErr: "filename is required",
		},
		{
			name: "local path",
			mutate: func(cfg *Config) {
				cfg.LocalPath = ""
			},
			wantErr: "local path is required",
		},
		{
			name: "exclude path relative",
			mutate: func(cfg *Config) {
				cfg.ExcludePaths = []string{"relative/path"}
			},
			wantErr: "exclude path must be absolute",
		},
		{
			name: "exclude path empty",
			mutate: func(cfg *Config) {
				cfg.ExcludePaths = []string{""}
			},
			wantErr: "exclude path must be absolute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidConfig()
			tt.mutate(cfg)

			err := verifyConfig(cfg)
			if err == nil {
				t.Fatalf("verifyConfig() error = nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("verifyConfig() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestVerifyConfigValidationOrder(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "synology before ssh",
			mutate: func(cfg *Config) {
				cfg.Synology.IP = ""
				cfg.SSH.IP = ""
			},
			wantErr: "synology ip address is required",
		},
		{
			name: "synology ip before port",
			mutate: func(cfg *Config) {
				cfg.Synology.IP = ""
				cfg.Synology.Port = 0
			},
			wantErr: "synology ip address is required",
		},
		{
			name: "synology port before username",
			mutate: func(cfg *Config) {
				cfg.Synology.Port = -1
				cfg.Synology.Username = ""
			},
			wantErr: "invalid synology port number",
		},
		{
			name: "synology username before password",
			mutate: func(cfg *Config) {
				cfg.Synology.Username = ""
				cfg.Synology.Password = ""
			},
			wantErr: "synology username is required",
		},
		{
			name: "synology password before path",
			mutate: func(cfg *Config) {
				cfg.Synology.Password = ""
				cfg.Synology.Path = ""
			},
			wantErr: "synology password is required",
		},
		{
			name: "synology address before download worker",
			mutate: func(cfg *Config) {
				cfg.Synology.Path = ""
				cfg.DownloadWorker = 0
			},
			wantErr: "filestation path is required",
		},
		{
			name: "download worker before ssh",
			mutate: func(cfg *Config) {
				cfg.DownloadWorker = 0
				cfg.SSH.IP = ""
			},
			wantErr: "download worker must be positive",
		},
		{
			name: "ssh before yaml",
			mutate: func(cfg *Config) {
				cfg.SSH.IP = ""
				cfg.YAML.Filename = ""
			},
			wantErr: "ssh ip address is required",
		},
		{
			name: "yaml before local path",
			mutate: func(cfg *Config) {
				cfg.YAML.Filename = ""
				cfg.LocalPath = ""
			},
			wantErr: "filename is required",
		},
		{
			name: "local path before exclude path",
			mutate: func(cfg *Config) {
				cfg.LocalPath = ""
				cfg.ExcludePaths = []string{"relative/path"}
			},
			wantErr: "local path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidConfig()
			tt.mutate(cfg)

			err := verifyConfig(cfg)
			if err == nil {
				t.Fatalf("verifyConfig() error = nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("verifyConfig() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestVerifyConfigDisabledTypes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "download type disabled allows nil synology",
			mutate: func(cfg *Config) {
				cfg.DownloadType = "disabled"
				cfg.Synology = nil
			},
		},
		{
			name: "download type disabled allows zero worker",
			mutate: func(cfg *Config) {
				cfg.DownloadType = "disabled"
				cfg.DownloadWorker = 0
			},
		},
		{
			name: "download type disabled allows negative worker",
			mutate: func(cfg *Config) {
				cfg.DownloadType = "disabled"
				cfg.DownloadWorker = -1
			},
		},
		{
			name: "upload type disabled allows nil ssh",
			mutate: func(cfg *Config) {
				cfg.UploadType = "disabled"
				cfg.SSH = nil
			},
		},
		{
			name: "db type disabled allows nil yaml",
			mutate: func(cfg *Config) {
				cfg.DBType = "disabled"
				cfg.YAML = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidConfig()
			tt.mutate(cfg)

			if err := verifyConfig(cfg); err != nil {
				t.Fatalf("verifyConfig() error = %v, want nil", err)
			}
		})
	}
}

func TestVerifyConfigExcludePathsValid(t *testing.T) {
	tests := []struct {
		name         string
		excludePaths []string
	}{
		{
			name:         "nil exclude paths",
			excludePaths: nil,
		},
		{
			name:         "empty exclude paths",
			excludePaths: []string{},
		},
		{
			name:         "absolute exclude paths",
			excludePaths: []string{"/volume1/photo/@eaDir", "/volume1/photo/../photo/#recycle"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidConfig()
			cfg.ExcludePaths = tt.excludePaths

			if err := verifyConfig(cfg); err != nil {
				t.Fatalf("verifyConfig() error = %v, want nil", err)
			}
		})
	}
}

func TestInitConfigExistingFile(t *testing.T) {
	restoreDefaultConfig(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`download_type: synology
synology:
  ip: 10.0.0.1
  port: 5001
  username: existing-user
  password: existing-pass
  path: /existing/photo
upload_type: ssh
ssh:
  ip: 10.0.0.2
  port: 2222
  username: upload-user
  password: upload-pass
  path: /existing/upload
db_type: yaml
yaml:
  filename: existing.yaml
local_path: /existing/local
exclude_paths:
  - /volume1/photo/@eaDir
  - /volume1/photo/#recycle
spare_space: 2048
sync_cycle: 6
download_worker: 3
download_delay: 4
download_retry_delay: 5
download_retry_count: 6
upload_delay: 7
upload_retry_delay: 8
upload_retry_count: 9
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := &Config{
		DownloadType: "synology",
		Synology: &Address{
			IP:       "10.0.0.1",
			Port:     5001,
			Username: "existing-user",
			Password: "existing-pass",
			Path:     "/existing/photo",
		},
		UploadType: "ssh",
		SSH: &Address{
			IP:       "10.0.0.2",
			Port:     2222,
			Username: "upload-user",
			Password: "upload-pass",
			Path:     "/existing/upload",
		},
		DBType:             "yaml",
		YAML:               &FileDB{Filename: "existing.yaml"},
		LocalPath:          "/existing/local",
		ExcludePaths:       []string{"/volume1/photo/@eaDir", "/volume1/photo/#recycle"},
		SpareSpace:         2048,
		SyncCycle:          6,
		DownloadWorker:     3,
		DownloadDelay:      4,
		DownloadRetryDelay: 5,
		DownloadRetryCount: 6,
		UploadDelay:        7,
		UploadRetryDelay:   8,
		UploadRetryCount:   9,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadEmptyPathUsesExistingDefault(t *testing.T) {
	chdirTemp(t)
	restoreDefaultConfig(t)
	contents := []byte(`download_type: disabled
upload_type: disabled
db_type: disabled
local_path: /existing/default
spare_space: 4096
sync_cycle: 24
`)
	if err := os.WriteFile(DefaultConfigPath, contents, 0o600); err != nil {
		t.Fatalf("write default config: %v", err)
	}

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v, want nil", err)
	}
	want := &Config{
		DownloadType: "disabled",
		UploadType:   "disabled",
		DBType:       "disabled",
		LocalPath:    "/existing/default",
		SpareSpace:   4096,
		SyncCycle:    24,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(\"\") = %#v, want %#v", got, want)
	}
}

func TestLoadEmptyPathMissingCreatesDefault(t *testing.T) {
	chdirTemp(t)
	restoreDefaultConfig(t)
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	got, err := Load("")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load(\"\") error = %v, want os.ErrNotExist", err)
	}
	if got != nil {
		t.Fatalf("Load(\"\") config = %#v, want nil", got)
	}
	created, err := Load(DefaultConfigPath)
	if err != nil {
		t.Fatalf("Load(created default) error = %v, want nil", err)
	}
	want := currentDefaultConfig(workingDir)
	if !reflect.DeepEqual(created, want) {
		t.Fatalf("created default config = %#v, want %#v", created, want)
	}
}

func TestLoadExplicitMissingPathDoesNotUseExistingDefault(t *testing.T) {
	workingDir := chdirTemp(t)
	restoreDefaultConfig(t)
	contents := []byte(`download_type: disabled
upload_type: disabled
db_type: disabled
local_path: /existing/default
`)
	if err := os.WriteFile(DefaultConfigPath, contents, 0o600); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	missingPath := filepath.Join(workingDir, "missing.yaml")

	got, err := Load(missingPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load(explicit missing) error = %v, want os.ErrNotExist", err)
	}
	if got != nil {
		t.Fatalf("Load(explicit missing) config = %#v, want nil", got)
	}
	after, err := os.ReadFile(DefaultConfigPath)
	if err != nil {
		t.Fatalf("read existing default: %v", err)
	}
	if !reflect.DeepEqual(after, contents) {
		t.Fatalf("existing default after Load() = %q, want %q", after, contents)
	}
}

func TestInitConfigExcludePathsOptionalEmpty(t *testing.T) {
	tests := []struct {
		name     string
		contents []byte
	}{
		{
			name: "missing exclude paths",
			contents: []byte(`download_type: disabled
upload_type: disabled
db_type: disabled
local_path: /existing/local
`),
		},
		{
			name: "explicit empty exclude paths",
			contents: []byte(`download_type: disabled
upload_type: disabled
db_type: disabled
local_path: /existing/local
exclude_paths: []
`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreDefaultConfig(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, tt.contents, 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			got, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if len(got.ExcludePaths) != 0 {
				t.Fatalf("Load().ExcludePaths = %#v, want empty", got.ExcludePaths)
			}
		})
	}
}

func TestInitConfigMissingFile(t *testing.T) {
	chdirTemp(t)
	restoreDefaultConfig(t)
	path := filepath.Join(t.TempDir(), "missing.yaml")
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	got, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want missing-file error")
	}
	if got != nil {
		t.Fatalf("Load() config = %#v, want nil", got)
	}
	if defaultConfig.LocalPath != workingDir {
		t.Fatalf("default local path after missing config = %q, want %q", defaultConfig.LocalPath, workingDir)
	}
}

func TestInitConfigMalformedFile(t *testing.T) {
	restoreDefaultConfig(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("download_type: [\n"), 0o600); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}

	got, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want malformed YAML error")
	}
	if got != nil {
		t.Fatalf("Load() config = %#v, want nil", got)
	}
}

func TestInitConfigEmptyFile(t *testing.T) {
	restoreDefaultConfig(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	got, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want config-required error")
	}
	if err.Error() != "config is required" {
		t.Fatalf("Load() error = %q, want %q", err.Error(), "config is required")
	}
	if got != nil {
		t.Fatalf("Load() config = %#v, want nil", got)
	}
}

func TestMakeDefaultConfigCreatesCurrentSchema(t *testing.T) {
	chdirTemp(t)
	restoreDefaultConfig(t)
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	if _, err := Load(DefaultConfigPath); err == nil {
		t.Fatal("Load(missing default) error = nil, want missing-file error")
	}

	if err := makeDefaultConfig(); err != nil {
		t.Fatalf("makeDefaultConfig() error = %v, want nil", err)
	}

	got, err := Load(DefaultConfigPath)
	if err != nil {
		t.Fatalf("Load(default config) error = %v, want nil", err)
	}
	want := currentDefaultConfig(workingDir)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("created default config = %#v, want %#v", got, want)
	}
	if err := verifyConfig(got); err != nil {
		t.Fatalf("verifyConfig(created default) error = %v, want nil", err)
	}
	data, err := os.ReadFile(DefaultConfigPath)
	if err != nil {
		t.Fatalf("read default config: %v", err)
	}
	if strings.Contains(string(data), "exclude_paths") {
		t.Fatalf("default config contains exclude_paths, want omitted: %s", string(data))
	}
}

func TestInitConfigDoesNotOverwriteExistingDefault(t *testing.T) {
	chdirTemp(t)
	restoreDefaultConfig(t)

	want := []byte(`download_type: disabled
upload_type: disabled
db_type: disabled
local_path: /existing/local
`)
	if err := os.WriteFile(DefaultConfigPath, want, 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	if _, err := Load(DefaultConfigPath); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	got, err := os.ReadFile(DefaultConfigPath)
	if err != nil {
		t.Fatalf("read existing config: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("existing config after Load() = %q, want %q", got, want)
	}
}

func TestFileExistsTreatsOnlyNotExistAsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("download_type: disabled\n"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing file",
			path: path,
			want: true,
		},
		{
			name: "missing file",
			path: filepath.Join(t.TempDir(), "missing.yaml"),
			want: false,
		},
		{
			name: "stat error that is not missing",
			path: string([]byte{'b', 'a', 'd', 0, 'p', 'a', 't', 'h'}),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fileExists(tt.path); got != tt.want {
				t.Fatalf("fileExists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestCurrentDefaultConfigSchema(t *testing.T) {
	restoreDefaultConfig(t)
	want := currentDefaultConfig("")
	if !reflect.DeepEqual(defaultConfig, want) {
		t.Fatalf("defaultConfig = %#v, want %#v", defaultConfig, want)
	}
}

func chdirTemp(t *testing.T) string {
	t.Helper()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	return tempDir
}

func restoreDefaultConfig(t *testing.T) {
	t.Helper()

	saved := *defaultConfig
	if defaultConfig.Synology != nil {
		synology := *defaultConfig.Synology
		saved.Synology = &synology
	}
	if defaultConfig.SSH != nil {
		ssh := *defaultConfig.SSH
		saved.SSH = &ssh
	}
	if defaultConfig.YAML != nil {
		yaml := *defaultConfig.YAML
		saved.YAML = &yaml
	}
	t.Cleanup(func() {
		*defaultConfig = saved
	})
}

func currentDefaultConfig(localPath string) *Config {
	return &Config{
		DownloadType: "synology",
		Synology: &Address{
			IP:       "1.2.3.4",
			Port:     5001,
			Username: "admin",
			Password: "pass",
			Path:     "/photo",
		},
		UploadType: "ssh",
		SSH: &Address{
			IP:       "192.168.0.100",
			Port:     22,
			Username: "user",
			Password: "pass",
			Path:     "/DCIM",
		},
		DBType:             "yaml",
		YAML:               &FileDB{Filename: "metadata.yaml"},
		LocalPath:          localPath,
		SpareSpace:         1073741824,
		SyncCycle:          12,
		DownloadWorker:     runtime.GOMAXPROCS(0),
		DownloadDelay:      10,
		DownloadRetryDelay: 2,
		DownloadRetryCount: 10,
		UploadDelay:        10,
		UploadRetryDelay:   2,
		UploadRetryCount:   10,
	}
}
