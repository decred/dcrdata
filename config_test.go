package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/decred/dcrd/dcrutil"
)

func TestMain(m *testing.M) {
	// Temp config file is used to ensure there are no external influences
	// from previously set env variables or default config files.
	file, _ := ioutil.TempFile("", "dcrdata_test_file.cfg")
	defer os.Remove(file.Name())
	os.Setenv("DCRDATA_CONFIG_FILE", file.Name())

	// Parse the -test.* flags before removing them from the command line
	// arguments list, which we do to allow go-flags to succeed.
	flag.Parse()
	os.Args = os.Args[:1]
	// Run the tests now that the testing package flags have been parsed.
	m.Run()
	os.Setenv("DCRDATA_CONFIG_FILE", "")
}

func TestDefaultConfig(t *testing.T) {
	c := defaultConfig
	if c.ConfigFile != defaultConfigFile {
		t.Errorf("expected %s config %s ", c.ConfigFile, defaultConfigFile)
	}
}

func TestDefaultConfigFile(t *testing.T) {
	appdir := dcrutil.AppDataDir("dcrdata", false)
	dcrpath := filepath.Join(appdir, "dcrdata.conf")
	if defaultConfigFile != dcrpath {
		t.Errorf("expected %s config %s ", defaultConfigFile, dcrpath)
		t.Errorf("Failed to load default dcrdata config")
	}
}

func TestLoadConfig(t *testing.T) {
	_, err := loadConfig()
	if err != nil {
		t.Errorf("Failed to load dcrdata config: %s\n", err.Error())
	}
}

func TestDefaultConfigAPIListen(t *testing.T) {
	cfg, err := loadConfig()
	if err != nil {
		t.Errorf("Failed to load dcrdata config: %s\n", err.Error())
	} else {
		v := cfg.APIListen
		if v != "127.0.0.1:7777" {
			t.Errorf("config expected %s but returned %s", "127.0.0.1:7777", v)
		}
	}

}

func TestDefaultConfigAPIListenWithEnv(t *testing.T) {
	os.Setenv("DCRDATA_LISTEN_URL", "0.0.0.0:7777")
	cfg, _ := loadConfig()
	v := cfg.APIListen
	if v != "0.0.0.0:7777" {
		t.Errorf("config expected %s but returned %s", "0.0.0.0:7777", v)
	}
}

func TestDefaultConfigAppDataDir(t *testing.T) {
	appdir := dcrutil.AppDataDir("dcrdata", false)
	cfg, _ := loadConfig()
	v := cfg.HomeDir
	expected := appdir
	if v != expected {
		t.Errorf("config expected %s but returned %s", expected, v)
	}
}

func TestDefaultConfigHomeDirWithEnv(t *testing.T) {
	os.Setenv("DCRDATA_APPDATA_DIR", "/tmp/test")
	cfg, _ := loadConfig()
	v := cfg.HomeDir
	expected := "/tmp/test"
	if v != expected {
		t.Errorf("config expected %s but returned %s", expected, v)
	}
}

// Test to ensure that the flags override env variables.
func TestDefaultConfigHomeDirWithEnvAndFlag(t *testing.T) {
	os.Args = append(os.Args, "--appdata=/tmp/test2")
	os.Setenv("DCRDATA_APPDATA_DIR", "/tmp/test")
	cfg, _ := loadConfig()
	v := cfg.HomeDir
	expected := filepath.Join("/tmp/test2")
	if v != expected {
		t.Errorf("config expected %s but returned %s", expected, v)
	}
}

func TestDefaultConfigTestNet(t *testing.T) {
	cfg, _ := loadConfig()
	v := cfg.TestNet
	if v {
		t.Errorf("config expected true but returned %+v", v)
	}
}

func TestDefaultConfigTestNetWithEnv(t *testing.T) {
	os.Setenv("DCRDATA_USE_TESTNET", "true")
	cfg, _ := loadConfig()
	v := cfg.TestNet
	if !v {
		t.Errorf("config expected true but returned %+v", v)
	}
}

func TestDefaultConfigTestNetWithEnvAndBadValue(t *testing.T) {
	os.Setenv("DCRDATA_USE_TESTNET", "no")
	_, err := loadConfig()
	if err == nil {
		t.Errorf("Failed to throw error when bad value is passed")
	}
}
