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
	IsSkip   bool   `yaml:"is_skip"`
}

type Config struct {
	Synology   Address `yaml:"synology"`
	Remote     Address `yaml:"remote"`
	Local      Address `yaml:"local"`
	spareSpace int     `yaml:"spare_space"`
	syncCycle  int     `yaml:"sync_cycle"`

	downloadWorker     int `yaml:"download_worker"`
	downloadDelay      int `yaml:"download_delay"`
	downloadRetryDelay int `yaml:"download_retry_delay"`
	downloadRetryCount int `yaml:"download_retry_count"`

	uploadWocker     int `yaml:"upload_wocker"`
	uploadDelay      int `yaml:"upload_delay"`
	uploadRetryDelay int `yaml:"upload_retry_delay"`
	uploadRetryCount int `yaml:"upload_retry_count"`
}

var defaultConfig = Config{
	Synology: Address{
		IP:       "1.2.3.4", // FileStation IP address
		Port:     "5001",    // FileStation port
		Username: "admin",   // FileStation account username
		Password: "pass",    // FileStation account password
		Path:     "/photo",  // FileStation path to download files
		IsSkip:   false,     // Skip Option
	},
	Remote: Address{
		IP:       "192.168.0.100", // Remote SSH IP address
		Port:     "22",            // Remote SSH port
		Username: "user",          // Remote SSH username
		Password: "pass",          // Remote SSH password
		Path:     "/DCIM",         // Remote path to download files
		IsSkip:   false,           // Skip Option
	},
	Local: Address{
		Path: "", // Local path to save download files (os.Getwd())
	},
	spareSpace: 1073741824, // 1GByte
	syncCycle:  12,         // Hour

	downloadWorker:     runtime.GOMAXPROCS(0),
	downloadDelay:      10, // Second
	downloadRetryDelay: 2,  // Second
	downloadRetryCount: 10,

	uploadWocker:     runtime.GOMAXPROCS(0),
	uploadDelay:      10, // Second
	uploadRetryDelay: 2,  // Second
	uploadRetryCount: 10,
}

const defaultConfigPath = "./config.yaml"

func initConfig(configPath string) (*Config, error) {
	defaultConfig.Local.Path, _ = os.Getwd()
	var result *Config

	// 설정 파일 확인
	if protocol.FileExists(configPath) {
		// 파일 읽기
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("fail to read %s config file: %v", configPath, err)
		}

		// YAML 언마샬링
		if err := yaml.Unmarshal(data, result); err != nil {
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
	if !config.Synology.IsSkip {
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

	// verify remote
	if !config.Remote.IsSkip {
		// verify ip address
		if len(config.Remote.IP) == 0 {
			return errors.New("remote ip address is required")
		}
		// verify port number
		if len(config.Remote.Port) == 0 {
			return errors.New("remote port is required")
		}
		if _, err := net.LookupPort("tcp", config.Remote.Port); err != nil {
			return errors.New("invalid remote port number")
		}
		// verify username and password
		if len(config.Remote.Username) == 0 {
			return errors.New("remote username is required")
		}
		if len(config.Remote.Password) == 0 {
			return errors.New("remote password is required")
		}
		// verify path
		if len(config.Remote.Path) == 0 {
			return errors.New("remote path is required")
		}
	}

	// verify local
	if len(config.Local.Path) == 0 {
		return errors.New("local path is required")
	}

	return nil
}
