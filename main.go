package main

import (
	"fmt"
	"os"

	"gopkg.in/ini.v1"
)

type AppConfig struct {
	DbName          string
	DbUser          string
	DbPassword      string
	DbHost          string
	DbPort          int
	TableName       string
	EmailColumnName string
}

func readConfig() (*AppConfig, error) {
	cfg, err := ini.Load("config.txt")
	if err != nil {
		return nil, err
	}

	rootSection := cfg.Section("")

	config := &AppConfig{}

	config.DbName = rootSection.Key("db_name").String()
	config.DbUser = rootSection.Key("db_user").String()
	config.DbPassword = rootSection.Key("db_password").String()
	config.DbHost = rootSection.Key("db_host").String()
	config.DbPort = rootSection.Key("db_port").MustInt(5432)
	config.TableName = rootSection.Key("table_name").String()
	config.EmailColumnName = rootSection.Key("email_column_name").String()

	return config, nil
}

func main() {
	_, err := readConfig()
	if err != nil {
		fmt.Printf("Fail to read config file: %v\n", err)
		os.Exit(1)
	}
}
