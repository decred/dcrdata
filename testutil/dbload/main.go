package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/decred/dcrdata/v4/testutil/dbload/testsconfig"
	"github.com/go-pg/migrations"
	"github.com/go-pg/pg"
)

const usageText = `This program runs command on the db. Supported commands are:
  - up - runs all available migrations.
  - up [target] - runs available migrations up to the target one.
  - down - reverts last migration.
  - reset - reverts all migrations.
  - version - prints current db version.
  - set_version [version] - sets db version without running migrations.
Usage:
  go run *.go <command> [args]
`

// If posgresql migrations are placed in this folder, this program picks them up
// automatically. https://github.com/go-pg/migrations#sql-migrations.

// Before running the tool, the appropriate test db should be created by.
// psql -c "CREATE DATABASE {{db-name}}". The db name is set at
// testconfig.PGChartsTestsDBName

func main() {
	if err := copyPgFileDumpHere(); err != nil {
		exitf(err.Error())
	}

	flag.Usage = usage
	flag.Parse()

	db := pg.Connect(&pg.Options{
		User:     testsconfig.PGChartsTestsUser,
		Database: testsconfig.PGChartsTestsDBName,
	})

	oldVersion, newVersion, err := migrations.Run(db, flag.Args()...)
	if err != nil {
		exitf(err.Error())
	}
	if newVersion != oldVersion {
		fmt.Printf("migrated from version %d to %d\n", oldVersion, newVersion)
	} else {
		fmt.Printf("version is %d\n", oldVersion)
	}
}

func usage() {
	fmt.Print(usageText)
	flag.PrintDefaults()
	os.Exit(2)
}

func errorf(s string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, s+"\n", args...)
}

func exitf(s string, args ...interface{}) {
	errorf(s, args...)
	os.Exit(1)
}

// copys the pg dump data file to this folder
func copyPgFileDumpHere() error {
	dir, err := testsconfig.TempDir()
	if err != nil {
		return err
	}

	dst := "data.up.sql"

	// if the file already exists quit.
	if _, err = os.Stat(dst); os.IsExist(err) {
		return nil
	}

	bufferSize := 1000
	src := filepath.Join(dir, "pgdb", "data.sql")

	source, err := os.Open(src)
	if err != nil {
		return err
	}

	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}

	defer destination.Close()

	buf := make([]byte, bufferSize)
	for {
		n, err := source.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if _, err := destination.Write(buf[:n]); err != nil {
			return err
		}
	}
	return nil
}
