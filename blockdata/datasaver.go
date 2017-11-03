// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

// Interface for saving/storing BlockData.
// Create a BlockDataSaver by implementing Store(*BlockData).

package blockdata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/decred/dcrd/wire"
)

// BlockDataSaver is an interface for saving/storing BlockData
type BlockDataSaver interface {
	Store(*BlockData, *wire.MsgBlock) error
}

////////////////////////////////////////////////////////////////////////////////
// The rest of the file contains some simple savers.  MAKE YOUR OWN to satisfy
// BlockDataSaver.
// /////////////////////////////////////////////////////////////////////////////

// BlockDataToJSONStdOut implements BlockDataSaver interface for JSON output to
// stdout
type BlockDataToJSONStdOut struct {
	mtx *sync.Mutex
}

// BlockDataToSummaryStdOut implements BlockDataSaver interface for plain text
// summary to stdout
type BlockDataToSummaryStdOut struct {
	mtx          *sync.Mutex
	sdiffWinSize int64
}

type fileSaver struct {
	folder   string
	nameBase string
	file     os.File
	mtx      *sync.Mutex
}

// BlockDataToJSONFiles implements BlockDataSaver interface for JSON output to
// the file system
type BlockDataToJSONFiles struct {
	fileSaver
}

// BlockDataToMySQL implements BlockDataSaver interface for output to a
// MySQL database
// type BlockDataToMySQL struct {
// 	mtx *sync.Mutex
// }

// NewBlockDataToJSONStdOut creates a new BlockDataToJSONStdOut with optional
// existing mutex
func NewBlockDataToJSONStdOut(m ...*sync.Mutex) *BlockDataToJSONStdOut {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	if len(m) > 0 {
		return &BlockDataToJSONStdOut{m[0]}
	}
	return &BlockDataToJSONStdOut{}
}

// NewBlockDataToSummaryStdOut creates a new BlockDataToSummaryStdOut with
// optional existing mutex
func NewBlockDataToSummaryStdOut(sdiffWinSize int64, m ...*sync.Mutex) *BlockDataToSummaryStdOut {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	if len(m) > 0 {
		return &BlockDataToSummaryStdOut{m[0], sdiffWinSize}
	}
	return &BlockDataToSummaryStdOut{sdiffWinSize: sdiffWinSize}
}

// NewBlockDataToJSONFiles creates a new BlockDataToJSONFiles with optional
// existing mutex
func NewBlockDataToJSONFiles(folder string, fileBase string,
	m ...*sync.Mutex) *BlockDataToJSONFiles {
	if len(m) > 1 {
		panic("Too many inputs.")
	}

	var mtx *sync.Mutex
	if len(m) > 0 {
		mtx = m[0]
	} else {
		mtx = new(sync.Mutex)
	}

	return &BlockDataToJSONFiles{
		fileSaver: fileSaver{
			folder:   folder,
			nameBase: fileBase,
			file:     os.File{},
			mtx:      mtx,
		},
	}
}

// Store writes BlockData to stdout in JSON format
func (s *BlockDataToJSONStdOut) Store(data *BlockData, _ *wire.MsgBlock) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	// Marshall all the block data results in to a single JSON object, indented
	jsonConcat, err := JSONFormatBlockData(data)
	if err != nil {
		return err
	}

	// Write JSON to stdout with guards to delimit the object from other text
	fmt.Printf("\n--- BEGIN BlockData JSON ---\n")
	_, err = writeFormattedJSONBlockData(jsonConcat, os.Stdout)
	fmt.Printf("--- END BlockData JSON ---\n\n")

	return err
}

// Store writes BlockData to stdout as plain text summary
func (s *BlockDataToSummaryStdOut) Store(data *BlockData, _ *wire.MsgBlock) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	fmt.Printf("\nBlock %v:\n", data.Header.Height)

	fmt.Printf("  Stake difficulty:                 %9.3f -> %.3f (current -> next block)\n",
		data.CurrentStakeDiff.CurrentStakeDifficulty,
		data.CurrentStakeDiff.NextStakeDifficulty)

	fmt.Printf("  Estimated price in next window:   %9.3f / [%.2f, %.2f] ([min, max])\n",
		data.EstStakeDiff.Expected, data.EstStakeDiff.Min, data.EstStakeDiff.Max)
	fmt.Printf("  Window progress:   %3d / %3d in price window number %v\n",
		data.IdxBlockInWindow, s.sdiffWinSize, data.PriceWindowNum)

	fmt.Printf("  Ticket fees:  %.4f, %.4f, %.4f (mean, median, std), n=%d\n",
		data.FeeInfo.Mean, data.FeeInfo.Median, data.FeeInfo.StdDev,
		data.FeeInfo.Number)

	if data.PoolInfo.Value >= 0 {
		fmt.Printf("  Ticket pool:  %v (size), %.3f (avg. price), %.2f (total DCR locked)\n",
			data.PoolInfo.Size, data.PoolInfo.ValAvg, data.PoolInfo.Value)
	}

	fmt.Printf("  Node connections:  %d\n", data.Connections)

	return nil
}

// Store writes BlockData to a file in JSON format
// The file name is nameBase+height+".json".
func (s *BlockDataToJSONFiles) Store(data *BlockData, _ *wire.MsgBlock) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	// Marshall all the block data results in to a single JSON object, indented
	jsonConcat, err := JSONFormatBlockData(data)
	if err != nil {
		return err
	}

	// Write JSON to a file with block height in the name
	height := data.Header.Height
	fname := fmt.Sprintf("%s%d.json", s.nameBase, height)
	fullfile := filepath.Join(s.folder, fname)
	fp, err := os.Create(fullfile)
	if err != nil {
		log.Errorf("Unable to open file %v for writing.", fullfile)
		return err
	}
	defer fp.Close()

	s.file = *fp
	_, err = writeFormattedJSONBlockData(jsonConcat, &s.file)

	return err
}

func writeFormattedJSONBlockData(jsonConcat *bytes.Buffer, w io.Writer) (int, error) {
	n, err := fmt.Fprintln(w, jsonConcat.String())
	// there was once more, perhaps again.
	return n, err
}

// JSONFormatBlockData concatenates block data results into a single JSON
// object with primary keys for the result type
func JSONFormatBlockData(data *BlockData) (*bytes.Buffer, error) {
	var jsonAll bytes.Buffer

	jsonAll.WriteString("{\"estimatestakediff\": ")
	stakeDiffEstJSON, err := json.Marshal(data.EstStakeDiff)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(stakeDiffEstJSON)
	//stakeDiffEstJSON, err := json.MarshalIndent(data.eststakediff, "", "    ")
	//fmt.Println(string(stakeDiffEstJSON))

	jsonAll.WriteString(",\"currentstakediff\": ")
	stakeDiffJSON, err := json.Marshal(data.CurrentStakeDiff)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(stakeDiffJSON)

	jsonAll.WriteString(",\"ticketfeeinfo_block\": ")
	feeInfoJSON, err := json.Marshal(data.FeeInfo)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(feeInfoJSON)

	jsonAll.WriteString(",\"block_header\": ")
	blockHeaderJSON, err := json.Marshal(data.Header)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(blockHeaderJSON)

	jsonAll.WriteString(",\"ticket_pool_info\": ")
	poolInfoJSON, err := json.Marshal(data.PoolInfo)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(poolInfoJSON)

	jsonAll.WriteString("}")

	var jsonAllIndented bytes.Buffer
	err = json.Indent(&jsonAllIndented, jsonAll.Bytes(), "", "    ")
	if err != nil {
		return nil, err
	}

	return &jsonAllIndented, err
}
