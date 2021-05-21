package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var logFILE *os.File

var outw io.Writer

const logFile = "swapscan-btc.txt"

// InitLogger starts the logger
func InitLogger() error {
	logFilePath, _ := filepath.Abs(logFile)
	var err error
	logFILE, err = os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND,
		0664)
	if err != nil {
		return fmt.Errorf("Error opening log file: %v", err)
	}

	outw = io.MultiWriter(logFILE, os.Stdout)

	return nil
}
