package testutil

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
)

func ToJson(object interface{}) string {
	bytes, err := json.MarshalIndent(object, "", "    ")
	if err != nil {
		ReportTestIsNotAbleToTest(
			"Failed to produce json for %v: %v",
			object,
			err)
	}
	jsonString := string(bytes[:])
	return jsonString
}

func FromJson(jsonString string, object interface{}) {
	err := json.Unmarshal([]byte(jsonString), object)
	if err != nil {
		ReportTestIsNotAbleToTest(
			"Failed to parse json for %v: %v \n json: \n%v",
			object,
			err,
			jsonString)
	}
}

func WriteStringToFile(targetFile string, jsonString string) {
	parent := filepath.Dir(targetFile)
	err := os.MkdirAll(parent, 0755)
	if err != nil {
		ReportTestIsNotAbleToTest(
			"Failed to read write %v: %v",
			targetFile,
			err)
	}
	err = ioutil.WriteFile(targetFile, []byte(jsonString), 0777)
	if err != nil {
		ReportTestIsNotAbleToTest(
			"Failed to write file %v: %v",
			targetFile,
			err)
	}
}

func ReadFileToString(targetFile string) string {
	bytes, err := ioutil.ReadFile(targetFile)
	if err != nil {
		ReportTestIsNotAbleToTest(
			"Failed to read file %v: %v",
			targetFile,
			err)
	}
	string := string(bytes[:])
	return string
}

func FullPathToFile(relative string) string {
	abs, err := filepath.Abs(relative)
	if err != nil {
		ReportTestIsNotAbleToTest(
			"Failed to build abs path for %v: %v",
			relative,
			err)
	}
	return abs
}

func MD5ofString(str string) string {
	data := []byte(str)
	md5arr := md5.Sum(data)
	md5String := hex.EncodeToString(md5arr[:])
	return md5String
}
