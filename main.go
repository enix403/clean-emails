package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"io"
	"strings"
	// "encoding/json"

	_ "github.com/lib/pq"
	"gopkg.in/ini.v1"

	emailverifier "github.com/AfterShip/email-verifier"
	progressbar "github.com/schollz/progressbar/v3"
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
var debugMode bool = false
var logger *log.Logger

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

func countEmails() int {
	query := fmt.Sprintf("SELECT COUNT(*) from \"%s\"", config.TableName)
	rows, err := db.Query(query)
	defer rows.Close()

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for rows.Next() {
		var count int
		rows.Scan(&count)
		return count
	}

	return 0
}

func dedupAction() {
	countBefore := countEmails()
	query := fmt.Sprintf(`
			DELETE FROM "%s"
			WHERE ctid IN (
			  SELECT ctid
			  FROM (
			    SELECT
			      ctid,
			      ROW_NUMBER() OVER (PARTITION BY "%s" ORDER BY ctid) AS row_num
			    FROM
			      "%s"
			  ) AS duplicates
			  WHERE row_num > 1
			);
		`,
		config.TableName,
		config.EmailColumnName,
		config.TableName,
	)

	rows, err := db.Query(query)
	defer rows.Close()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	countAfter := countEmails()

	numRemoved := countBefore - countAfter

	fmt.Printf("Removed duplicates: %d row(s) deleted\n", numRemoved)
	logger.Printf("Removed duplicates: %d row(s) deleted\n", numRemoved)
}

// 0 for valid
type FailureMask uint32

const (
	VFAIL_NULL FailureMask = 1 << iota
	VFAIL_SYNTAX
	VFAIL_MX
	VFAIL_DISPOSABLE
	VFAIL_SMTP
	VFAIL_CATCH_ALL
)

var failBitToString = map[FailureMask]string{
	VFAIL_NULL:       "NullEmail",
	VFAIL_SYNTAX:     "InvalidSyntax",
	VFAIL_DISPOSABLE: "Disposable",
	VFAIL_MX:         "NoMXRecord",
	VFAIL_SMTP:       "SMTPFailed",
}

func (mask FailureMask) ToReadable() string {
	val, ok := failBitToString[mask]
	if !ok {
		val = "Unknown error"
	}
	return val
	/*
		var sb strings.Builder

		started := false

		for failure, failureStr := range failBitToString {
			if mask&failure != 0 {
				if started {
					sb.WriteByte('+')
				} else {
					started = true
				}
				sb.WriteString(failureStr)
			}
		}

		// final := strings.TrimRight(sb.String(), "+")
		return sb.String()
	*/

}

var verifier = emailverifier.NewVerifier()

func validateEmail(email string, smtpEnabled bool) FailureMask {
	email = strings.TrimSpace(email)

	if email == "" {
		return VFAIL_NULL
	}

	ret, _ := verifier.Verify(email)

	if !ret.Syntax.Valid {
		return VFAIL_SYNTAX
	}

	if ret.Disposable {
		return VFAIL_DISPOSABLE
	}

	if !ret.HasMxRecords {
		return VFAIL_MX
	}

	if smtpEnabled {
		if ret.SMTP == nil {
			return VFAIL_SMTP
		}
	}

	return 0
}

const STATUS_COLUMN_NAME string = "Status"

func ensureStatusColumn() {
	query := fmt.Sprintf("SELECT column_name FROM information_schema.columns where table_name = '%s'", config.TableName)
	rows, err := db.Query(query)
	defer rows.Close()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	found := false

	for rows.Next() {
		var colname string
		rows.Scan(&colname)

		if colname == STATUS_COLUMN_NAME {
			found = true
			break
		}
	}

	if found {
		return
	}

	alter_query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN \"%s\" TEXT;", config.TableName, STATUS_COLUMN_NAME)
	rows, err = db.Query(alter_query)
	defer rows.Close()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func setEmailStatus(email string, status string) {
	query := fmt.Sprintf(
		"UPDATE app_table SET \"%s\" = '%s' WHERE \"%s\" = '%s'",
		STATUS_COLUMN_NAME,
		status,
		config.EmailColumnName,
		email)

	rows, err := db.Query(query)
	defer rows.Close()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func validateAction(enableSMPTCheck bool, proxy string) {

	ensureStatusColumn()

	if enableSMPTCheck {
		verifier.EnableSMTPCheck()
	}

	if proxy != "" {
		verifier.Proxy(proxy)
	}

	query := fmt.Sprintf("SELECT \"%s\" FROM \"%s\"", config.EmailColumnName, config.TableName)
	rows, err := db.Query(query)
	defer rows.Close()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	countValid := 0
	countInvalid := 0

	totalEmails := countEmails()
	bar := progressbar.Default(int64(totalEmails))

	for rows.Next() {
		var email string
		rows.Scan(&email)

		if failures := validateEmail(email, enableSMPTCheck); failures != 0 {
			reason := failures.ToReadable()
			// fmt.Printf("%s -> %s\n", email, reason)
			setEmailStatus(email, "Failed: "+reason)
			countInvalid++
		} else {
			setEmailStatus(email, "Valid")
			countValid++
		}
		bar.Add(1)
	}

	fmt.Printf("Validation Complete: %d valid, %d invalid\n", countValid, countInvalid)
	logger.Printf("Validation Complete: %d valid, %d invalid", countValid, countInvalid)
}

func main() {
	parser := argparse.NewParser("clean-emails", "Email Cleaner")

	configPathPtr := parser.StringPositional(&argparse.Options{
		Help:     "Path of config file",
		Required: true,
	})

	debugPtr := parser.Flag("", "debug", &argparse.Options{
		Help:     "Enable debug mode",
		Required: false,
	})

	dedupPtr := parser.Flag("p", "dedup", &argparse.Options{
		Help:     "Remove duplicate emails",
		Required: false,
	})

	validatePtr := parser.Flag("v", "validate", &argparse.Options{
		Help:     "Validate existing emails",
		Required: false,
	})

	enableSMPTPtr := parser.Flag("", "enable-smtp", &argparse.Options{
		Help:     "Enable SMTP checks. Effective with --validate option only.",
		Required: false,
	})

	// This sets a SOCKS5 proxy to verify the email.
	// Value has to be in the format: "socks5://user:password@127.0.0.1:1080?timeout=5s".
	// The protocol could be socks5, socks4 and socks4a.
	proxyPtr := parser.String("", "proxy", &argparse.Options{
		Help:     "SOCKS proxy for SMPT checks. Effective with --enable-smtp option only.",
		Required: false,
	})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	//fmt.Printf("configPath -> %v\n", *configPathPtr)
	//fmt.Printf("dedup -> %v\n", *dedupPtr)
	//fmt.Printf("validate -> %v\n", *validatePtr)
	//fmt.Printf("enableSMPT -> %v\n", *enableSMPTPtr)
	//fmt.Printf("proxy -> %v\n", *proxyPtr)

	dedup := *dedupPtr
	validate := *validatePtr

	if validate == dedup {
		fmt.Println("Provide exacty one of --validate and --dedup flags")
		os.Exit(1)
	}

	var configPath string = ""
	if configPathPtr != nil {
		configPath = *configPathPtr
	}

	if configPath == "" {
		fmt.Println("Provide config file path")
		os.Exit(1)
	}

	debugMode = *debugPtr

	logger = log.New(io.Discard, "INFO: ", log.Ldate|log.Ltime)

	if debugMode {
		logfile, err := os.Create("debug.log")
		if err != nil {
			log.Fatal(err)
		}
		defer logfile.Close()
		logger.SetOutput(logfile)
	}

	// Set global config
	config = readConfig(configPath)

	createDBConnection()

	if dedup {
		logger.Println("De duplicating emails")
		dedupAction()
	} else { // validate
		logger.Println("Validating emails")
		var proxy string = ""
		if proxyPtr != nil {
			proxy = *proxyPtr
		}
		validateAction(*enableSMPTPtr, proxy)
	}
}
