package main

import internalconfig "github.com/lolgopher/synology-filesync/internal/config"

type Address = internalconfig.Address
type DB = internalconfig.DB
type FileDB = internalconfig.FileDB
type Config = internalconfig.Config

var defaultConfig = internalconfig.DefaultConfig()

const defaultConfigPath = internalconfig.DefaultConfigPath

func initConfig(configPath string) (*Config, error) {
	return internalconfig.LoadWithDefault(configPath, defaultConfig)
}

func verifyConfig(config *Config) error {
	return internalconfig.Validate(config)
}

func makeDefaultConfig() error {
	return internalconfig.MakeDefaultConfigWithDefault(defaultConfig)
}
