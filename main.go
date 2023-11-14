package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
	"gopkg.in/ini.v1"

	"github.com/akamensky/argparse"
)

func take[T any](_ T) {}

type AppConfig struct {
	DbName          string
	DbUser          string
	DbPassword      string
	DbHost          string
	DbPort          int
	TableName       string
	EmailColumnName string
}

func readConfig(path string) *AppConfig {
	cfg, err := ini.Load(path)
	if err != nil {
		fmt.Printf("Fail to read config file: %v\n", err)
		os.Exit(1)
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

	return config
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

func createDBConnection() {
	psqlInfo := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		config.DbHost, config.DbPort, config.DbUser, config.DbPassword, config.DbName)

	conn, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		fmt.Printf("Fail to connect to the database: %v\n", err)
		os.Exit(1)
	}

	db = conn
}

func main() {
	parser := argparse.NewParser("print", "Prints provided string to stdout")

	configPathPtr := parser.StringPositional(&argparse.Options{
		Help:     "Path of config file",
		Required: true,
	})

	dedupPtr := parser.Flag("p", "dedup", &argparse.Options{
		Help:     "Remove duplicate emails",
		Required: false,
	})

	validatePtr := parser.Flag("v", "validate", &argparse.Options{
		Help:     "Validate existing emails",
		Required: false,
	})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	configPath := *configPathPtr
	dedup := *dedupPtr
	validate := *validatePtr

	if validate == dedup {
		fmt.Println("Provide exacty one of --validate and --dedup flags")
		os.Exit(1)
	}

	// Set global config
	config = readConfig(configPath)

	createDBConnection()
}
