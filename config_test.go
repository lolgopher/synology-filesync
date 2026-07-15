package main

import "testing"

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
		LocalPath: "/tmp/synology-filesync",
	}
}

func TestVerifyConfigValid(t *testing.T) {
	if err := verifyConfig(newValidConfig()); err != nil {
		t.Fatalf("verifyConfig() error = %v, want nil", err)
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
			name: "ssh ip",
			mutate: func(cfg *Config) {
				cfg.SSH.IP = ""
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
			name: "local path",
			mutate: func(cfg *Config) {
				cfg.LocalPath = ""
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
