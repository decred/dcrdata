package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shiena/ansicolor"
	// "github.com/mattn/go-colorable"
	prefix_fmt "github.com/chappjc/logrus-prefix"
	"github.com/sirupsen/logrus"
)

var logFILE *os.File

//var log = logrus.New()
var log *logrus.Logger

const logFile = "rebuilddb.log"

// InitLogger starts the logger
func InitLogger() error {
	logFilePath, _ := filepath.Abs(logFile)
	logFILE, err := os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND,
		0664)
	if err != nil {
		return fmt.Errorf("Error opening log file: %v", err)
	}

	logrus.SetOutput(io.MultiWriter(logFILE, os.Stdout))
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&prefix_fmt.TextFormatter{
		ForceColors:     true,
		ForceFormatting: true,
		FullTimestamp:   true,
	})
	//logrus.SetOutput(ansicolor.NewAnsiColorWriter(os.Stdout))
	//logrus.SetOutput(colorable.NewColorableStdout())

	log = logrus.New()
	log.Level = logrus.DebugLevel
	log.Formatter = &prefix_fmt.TextFormatter{
		ForceColors:     true,
		ForceFormatting: true,
		FullTimestamp:   true,
		TimestampFormat: "02 Jan 06 15:04.00 -0700",
	}

	//log.Out = colorable.NewColorableStdout()
	//log.Out = colorable.NewNonColorable(io.MultiWriter(logFILE, os.Stdout))
	log.Out = ansicolor.NewAnsiColorWriter(io.MultiWriter(logFILE, os.Stdout))

	log.Debug("rebuilddb logger started.")

	return nil
}
