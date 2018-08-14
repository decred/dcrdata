package testutil

import (
	"path/filepath"
)

const (
	TestDataFolderName     = "dcrdata-testdata"
	TestDescriptorFileName = "test-description.json"
)

type TestDataHandler struct {
	expected TestDescriptorJson
}

type TestDescriptorJson struct {
	TestName                                string `json:"test_name"`
	ExpectedBestBlockHeight                 int64  `json:"expected_block_height"`
	ExpectedStakeInfoHeight                 int64  `json:"expected_stake_info_height"`
	ExpectedBestBlockHash                   string `json:"expected_best_block_hash"`
	ExpectedMD5OfRetrieveBlockFeeInfoString string `json:"expected_md5_of_retrieve_block_fee_info_string"`
	FirstBlockNumber                        uint64 `json:"first_block_number"`
	LastBlockNumber                         uint64 `json:"last_block_number"`
}

func (db *TestDataHandler) GetFirstBlockNumber() uint64 {
	return db.expected.FirstBlockNumber
}

func (db *TestDataHandler) GetLastBlockNumber() uint64 {
	return db.expected.LastBlockNumber
}

func (db *TestDataHandler) GetExpectedBestBlockHeight() int64 {
	return db.expected.ExpectedBestBlockHeight
}

func (db *TestDataHandler) GetExpectedStakeInfoHeight() int64 {
	return db.expected.ExpectedStakeInfoHeight
}

func (db *TestDataHandler) GetExpectedBestBlockHash() string {
	return db.expected.ExpectedBestBlockHash
}
func (db *TestDataHandler) GetExpectedMD5OfRetrieveBlockFeeInfoString() string {
	return db.expected.ExpectedMD5OfRetrieveBlockFeeInfoString
}

func LoadTestDataHandler(tag string) *TestDataHandler {
	var tdb TestDataHandler
	testDescriptorFile := PathToTestDescriptorFile(tag)

	Log("reading:")

	Log("testDescriptorFile", testDescriptorFile)
	// uncomment this to produce example descriptor file
	//ProduceExampleTestDescriptor(PathToTestDescriptorFile(tag))

	testDescriptor := ReadTestDescriptorJson(testDescriptorFile)
	tdb = TestDataHandler{
		expected: testDescriptor,
	}

	return &tdb
}
func parseTestDescriptorJson(jsonString string) TestDescriptorJson {
	desc := TestDescriptorJson{}
	FromJson(jsonString, &desc)
	return desc
}
func ReadTestDescriptorJson(targetFile string) TestDescriptorJson {
	jsonString := ReadFileToString(targetFile)
	desc := parseTestDescriptorJson(jsonString)
	return desc
}

func PathToTestDataFolder(tag string) string {
	return FullPathToFile(
		filepath.Join(TestDataFolderName, tag))
}

func PathToTestDataFile(tag string, filename string) string {
	return FullPathToFile(
		filepath.Join(PathToTestDataFolder(tag), filename))
}

func PathToTestDescriptorFile(tag string) string {
	return PathToTestDataFile(tag, TestDescriptorFileName)
}

func ProduceExampleTestDescriptor(targetFile string) {
	exampleDescriptor := TestDescriptorJson{
		TestName:                                "exampleTest",
		ExpectedBestBlockHeight:                 -1,
		ExpectedStakeInfoHeight:                 -1,
		ExpectedBestBlockHash:                   "",
		ExpectedMD5OfRetrieveBlockFeeInfoString: "d41d8cd98f00b204e9800998ecf8427e",
	}

	jsonString := ToJson(exampleDescriptor)

	Log("writing", targetFile)
	Log(jsonString)
	WriteStringToFile(targetFile, jsonString)
}
