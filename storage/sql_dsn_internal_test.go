package storage

import (
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

var awkwardPasswords = []string{
	"",
	"plain",
	"has space",
	" leading",
	"it's",
	`back\slash`,
	`"double"`,
	"p@ss/w?x&y#z%20:",
	"x sslmode=disable host=evil",
}

func TestPostgresDSNKeepsValuesIntact(t *testing.T) {
	for _, password := range awkwardPasswords {
		dsn, err := postgresDSN(map[string]string{
			"host":     "db.internal",
			"port":     "5432",
			"user":     "o'brien",
			"password": password,
			"dbname":   "my db",
			"schema":   "public",
		})
		if err != nil {
			t.Fatalf("postgresDSN(%q): %v", password, err)
		}
		cfg, err := pgconn.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("password %q: parse: %v", password, err)
		}
		if cfg.Password != password || cfg.User != "o'brien" || cfg.Database != "my db" || cfg.Host != "db.internal" || cfg.Port != 5432 {
			t.Fatalf("password %q: got password=%q user=%q database=%q host=%q port=%d", password, cfg.Password, cfg.User, cfg.Database, cfg.Host, cfg.Port)
		}
		if len(cfg.RuntimeParams) != 0 {
			t.Fatalf("password %q: unexpected settings %v", password, cfg.RuntimeParams)
		}
	}
}

func TestPostgresDSNRejectsUnsafeKeys(t *testing.T) {
	for _, key := range []string{"", "bad key", "a=b", "x'"} {
		if _, err := postgresDSN(map[string]string{key: "v"}); err == nil {
			t.Fatalf("postgresDSN key %q = nil error; want it rejected", key)
		}
	}
}

func TestMySQLDSNKeepsValuesIntact(t *testing.T) {
	for _, password := range awkwardPasswords {
		dsn, err := mysqlDSN(map[string]string{
			"host":     "::1",
			"port":     "3306",
			"user":     "o'brien",
			"password": password,
			"dbname":   "app?parseTime=true",
		})
		if err != nil {
			t.Fatalf("mysqlDSN(%q): %v", password, err)
		}
		cfg, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			t.Fatalf("password %q: parse: %v", password, err)
		}
		if cfg.Passwd != password || cfg.User != "o'brien" || cfg.DBName != "app" || cfg.Addr != "[::1]:3306" || !cfg.ParseTime || !cfg.ClientFoundRows {
			t.Fatalf("password %q: got %+v", password, cfg)
		}
	}
}

func TestMySQLDSNRejectsInvalidOptions(t *testing.T) {
	_, err := mysqlDSN(map[string]string{"password": "s3cret", "dbname": "app?parseTime=maybe"})
	if err == nil {
		t.Fatal("mysqlDSN = nil error; want the invalid option rejected")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("error %q contains the password", err)
	}
}

func TestPostgresDSNParseErrorHidesPassword(t *testing.T) {
	for _, password := range []string{"SEKRITa' SEKRITb", "SEKRIT plain", `SEKRIT\ x`} {
		_, err := postgresDSN(map[string]string{"host": "h", "port": "not-a-port", "password": password})
		if err == nil {
			t.Fatalf("password %q: nil error; want the invalid port rejected", password)
		}
		if strings.Contains(err.Error(), "SEKRIT") {
			t.Fatalf("error %q contains the password", err)
		}
		if !strings.Contains(err.Error(), "invalid port") {
			t.Fatalf("error %q; want it to name the invalid port", err)
		}
	}
}

func TestMySQLDSNDecodesDBName(t *testing.T) {
	dsn, err := mysqlDSN(map[string]string{"host": "h", "port": "3306", "dbname": "my%20db/x?parseTime=true"})
	if err != nil {
		t.Fatalf("mysqlDSN: %v", err)
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse %q: %v", dsn, err)
	}
	if cfg.DBName != "my db/x" {
		t.Fatalf("DBName = %q; want %q", cfg.DBName, "my db/x")
	}

	if _, err := mysqlDSN(map[string]string{"dbname": "bad%zz"}); err == nil {
		t.Fatal("mysqlDSN = nil error; want the invalid escape rejected")
	}
}

func TestMySQLDSNRejectsColonInUser(t *testing.T) {
	if _, err := mysqlDSN(map[string]string{"user": "u:x", "password": "p"}); err == nil {
		t.Fatal("mysqlDSN = nil error; want a user name with ':' rejected")
	}
}
