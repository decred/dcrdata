package testutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
)

// BlockFilename generates file name for a Block
func BlockFilename(index uint64) string {
	return "block-" + padNumberWithZero(index, 10) + ".bin"
}

func padNumberWithZero(value uint64, zeroes uint64) string {
	return fmt.Sprintf("%0"+strconv.FormatUint(zeroes, 10)+"d", value)
}

// SaveBlockToFile writes Block to a file in the target folder
func SaveBlockToFile(block *dcrutil.Block, targetFolder string) (*string, error) {
	bytes, err := block.Bytes()
	if err != nil {
		Log(" failed", err)
		return nil, err
	}

	index := block.MsgBlock().Header.Height
	filename := filepath.Join(targetFolder, BlockFilename(uint64(index)))
	filename, err = filepath.Abs(filename)

	if err != nil {
		return nil, err
	}

	Log("writing", filename)
	parent := filepath.Dir(filename)
	err = os.MkdirAll(parent, 0755)
	if err != nil {
		Log(" failed", err)
		return nil, err
	}
	err = ioutil.WriteFile(filename, bytes, 0777)

	if err != nil {
		Log(" failed", err)
		return nil, err
	}

	return &filename, nil
}

// ReadBlock reads Block from file
func ReadBlock(file string) (*dcrutil.Block, error) {
	//Log("reading", file)
	file, err := filepath.Abs(file)

	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadFile(file)

	if err != nil {
		return nil, err
	}

	var msgBlock wire.MsgBlock
	err = msgBlock.Deserialize(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	block := dcrutil.NewBlock(&msgBlock)

	return block, nil
}

func PreLoadBlocks(tag string, fromIndex uint64, toIndex uint64) map[int64]*dcrutil.Block {
	testBlockchain := make(map[int64]*dcrutil.Block)
	for i := fromIndex; i <= toIndex; i++ {
		index := int64(i)
		blockFileName := BlockFilename(i)
		target := PathToTestDataFile(tag, blockFileName)
		block, err := ReadBlock(target)
		if err != nil {
			ReportTestIsNotAbleToTest("error reading", target)
		}

		testBlockchain[index] = block
		if block == nil {
			ReportTestIsNotAbleToTest("block not found", i)
		}
	}
	return testBlockchain
}
