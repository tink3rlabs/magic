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
		if cfg.Passwd != password || cfg.User != "o'brien" || cfg.DBName != "app" || cfg.Addr != "[::1]:3306" || !cfg.ParseTime {
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
