package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
	"gopkg.in/ini.v1"

	"github.com/cqroot/prompt"
	"github.com/cqroot/prompt/choose"
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

var db *sql.DB
var config *AppConfig

func emailList() {
	query := fmt.Sprintf("SELECT \"%s\" FROM \"%s\"",
		config.EmailColumnName, config.TableName)
	rows, err := db.Query(query)
	defer rows.Close()
	if err != nil {
		fmt.Println(err)
	}

	var email string
	for rows.Next() {
		rows.Scan(&email)
		fmt.Println(email)
	}
}

func startApp() {
	selection, _ := prompt.New().Ask("Select Action:").Choose(
		[]string{"De-Duplicate Emails", "Validate Emails", "Validate Emails With SMTP Check"},
		choose.WithTheme(choose.ThemeArrow),
	)

	fmt.Printf("{ %s }\n", selection)
}

func main() {
	localConfig, err := readConfig()
	if err != nil {
		fmt.Printf("Fail to read config file: %v\n", err)
		os.Exit(1)
	}

	config = localConfig

	psqlInfo := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		config.DbHost, config.DbPort, config.DbUser, config.DbPassword, config.DbName)

	conn, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		fmt.Printf("Fail to connect to the database: %v\n", err)
		os.Exit(1)
	}

	db = conn

	startApp()
}
