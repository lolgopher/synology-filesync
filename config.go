package main

import (
	"fmt"
	"github.com/lolgopher/synology-filesync/protocol"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"log"
	"net"
	"os"
	"runtime"
)

type Address struct {
	IP       string `yaml:"ip"`
	Port     string `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Path     string `yaml:"path"`
}

type DB struct {
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

type FileDB struct {
	Filename string `yaml:"filename"`
}

type Config struct {
	DownloadType string   `yaml:"download_type"`
	Synology     *Address `yaml:"synology,omitempty"`

	UploadType string   `yaml:"upload_type"`
	SSH        *Address `yaml:"ssh,omitempty"`

	DBType    string  `yaml:"db_type"`
	YAML      *FileDB `yaml:"yaml,omitempty"`
	LocalPath string  `yaml:"local_path"`

	SpareSpace     uint64 `yaml:"spare_space"`
	SyncCycle      int    `yaml:"sync_cycle"`
	DownloadWorker int    `yaml:"download_worker"`

	DownloadDelay      int `yaml:"download_delay"`
	DownloadRetryDelay int `yaml:"download_retry_delay"`
	DownloadRetryCount int `yaml:"download_retry_count"`

	UploadDelay      int `yaml:"upload_delay"`
	UploadRetryDelay int `yaml:"upload_retry_delay"`
	UploadRetryCount int `yaml:"upload_retry_count"`
}

var defaultConfig = &Config{
	DownloadType: "synology", // Download type(synology, skip(TBD), etc...(TBD))
	Synology: &Address{
		IP:       "1.2.3.4", // FileStation IP address
		Port:     "5001",    // FileStation port
		Username: "admin",   // FileStation account username
		Password: "pass",    // FileStation account password
		Path:     "/photo",  // FileStation path to download files
	},

	UploadType: "ssh", // Upload type(ssh, skip(TBD), etc...(TBD))
	SSH: &Address{
		IP:       "192.168.0.100", // SSH IP address
		Port:     "22",            // SSH port
		Username: "user",          // SSH username
		Password: "pass",          // SSH password
		Path:     "/DCIM",         // SSH path to download files
	},

	DBType: "yaml", // DB type(YAML, JSON(TBD), MySQL(TBD), etc...(TBD))
	YAML: &FileDB{
		Filename: "metadata.yaml", // FileDB filename
	},
	LocalPath: "", // Local path to save download files(os.Getwd())

	SpareSpace:     1073741824,            // Spare space of upload filesystem(Byte)
	SyncCycle:      12,                    // Sync cycle(Hour)
	DownloadWorker: runtime.GOMAXPROCS(0), // Number of concurrent downloads(runtime.GOMAXPROCS(0))

	DownloadDelay:      10, // Download delay(Second)(TBD)
	DownloadRetryDelay: 2,  // Download retry delay(Second)(TBD)
	DownloadRetryCount: 10, // Download retry count(TBD)

	UploadDelay:      10, // Upload delay(Second)
	UploadRetryDelay: 2,  // Upload retry delay(Second)
	UploadRetryCount: 10, // Upload retry count
}

const defaultConfigPath = "./config.yaml"

func initConfig(configPath string) (*Config, error) {
	defaultConfig.LocalPath, _ = os.Getwd()
	var result *Config

	// 설정 파일 확인
	if protocol.FileExists(configPath) {
		// 파일 읽기
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("fail to read %s config file: %v", configPath, err)
		}

		// YAML 언마샬링
		if err := yaml.Unmarshal(data, &result); err != nil {
			log.Printf("error to unmarshal read data: %s", string(data))
			return nil, fmt.Errorf("fail to unmarshal %s config file: %v", configPath, err)
		}
	} else {
		log.Printf("%s config file not found", configPath)

		// 기본 설정 파일 생성
		if !protocol.FileExists(defaultConfigPath) {
			if err := makeDefaultConfig(); err != nil {
				return nil, errors.Wrap(err, "fail to make default config file")
			}
		}
		return nil, os.ErrNotExist
	}

	return result, verifyConfig(result)
}

func makeDefaultConfig() error {
	configData, err := yaml.Marshal(defaultConfig)
	if err != nil {
		return errors.Wrap(err, "fail to marshal default config")
	}
	if err := os.WriteFile(defaultConfigPath, configData, 0644); err != nil {
		return fmt.Errorf("fail to write %s file: %v", defaultConfigPath, err)
	}
	log.Println("make default config file")
	return nil
}

func verifyConfig(config *Config) error {
	// verify synology
	if config.DownloadType == "synology" {
		// verify ip address
		if len(config.Synology.IP) == 0 {
			return errors.New("synology ip address is required")
		}
		// verify port number
		if len(config.Synology.Port) == 0 {
			return errors.New("synology port is required")
		}
		if _, err := net.LookupPort("tcp", config.Synology.Port); err != nil {
			return errors.New("invalid synology port number")
		}
		// verify username and password
		if len(config.Synology.Username) == 0 {
			return errors.New("synology username is required")
		}
		if len(config.Synology.Password) == 0 {
			return errors.New("synology password is required")
		}
		// verify path
		if len(config.Synology.Path) == 0 {
			return errors.New("filestation path is required")
		}
	}

	// verify ssh
	if config.UploadType == "ssh" {
		// verify ip address
		if len(config.SSH.IP) == 0 {
			return errors.New("ssh ip address is required")
		}
		// verify port number
		if len(config.SSH.Port) == 0 {
			return errors.New("ssh port is required")
		}
		if _, err := net.LookupPort("tcp", config.SSH.Port); err != nil {
			return errors.New("invalid ssh port number")
		}
		// verify username and password
		if len(config.SSH.Username) == 0 {
			return errors.New("ssh username is required")
		}
		if len(config.SSH.Password) == 0 {
			return errors.New("ssh password is required")
		}
		// verify path
		if len(config.SSH.Path) == 0 {
			return errors.New("ssh path is required")
		}
	}

	// verify yaml
	if config.DBType == "yaml" {
		if len(config.YAML.Filename) == 0 {
			return errors.New("filename is required")
		}
	}

	// verify local
	if len(config.LocalPath) == 0 {
		return errors.New("local path is required")
	}

	return nil
}
