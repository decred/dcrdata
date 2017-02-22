// Interface for saving/storing blockData.
// Create a BlockDataSaverby implementing Store(*blockData).

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	apitypes "github.com/chappjc/dcrdata/dcrdataapi"
)

type fileSaver struct {
	folder   string
	nameBase string
	file     os.File
	mtx      *sync.Mutex
}

// BlockDataSaver is an interface for saving/storing blockData
type BlockDataSaver interface {
	Store(data *blockData) error
}

type BlockDataToMemdb struct {
	mtx             *sync.Mutex
	Height          int
	blockDataMap    map[int]*blockData
	blockSummaryMap map[int]*apitypes.BlockDataBasic
}

// BlockDataToJSONStdOut implements BlockDataSaver interface for JSON output to
// stdout
type BlockDataToJSONStdOut struct {
	mtx *sync.Mutex
}

// BlockDataToSummaryStdOut implements BlockDataSaver interface for plain text
// summary to stdout
type BlockDataToSummaryStdOut struct {
	mtx *sync.Mutex
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

func NewBlockDataToMemdb(m ...*sync.Mutex) *BlockDataToMemdb {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	saver := &BlockDataToMemdb{
		Height:          -1,
		blockDataMap:    make(map[int]*blockData),
		blockSummaryMap: make(map[int]*apitypes.BlockDataBasic),
	}
	if len(m) > 0 {
		saver.mtx = m[0]
	}
	return saver
}

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
func NewBlockDataToSummaryStdOut(m ...*sync.Mutex) *BlockDataToSummaryStdOut {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	if len(m) > 0 {
		return &BlockDataToSummaryStdOut{m[0]}
	}
	return &BlockDataToSummaryStdOut{}
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

// Store writes blockData to memdb
func (s *BlockDataToMemdb) Store(data *blockData) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	s.Height = int(data.header.Height)

	// save data to slice in memory
	s.blockDataMap[s.Height] = data

	blockSummary := apitypes.BlockDataBasic{
		Height:         uint32(s.Height),
		Size:           data.header.Size,
		Difficulty:     data.header.Difficulty,
		StakeDiff:      data.header.SBits,
		Time:           data.header.Time,
		TicketPoolInfo: data.poolinfo,
	}
	s.blockSummaryMap[s.Height] = &blockSummary

	return nil
}

func (s *BlockDataToMemdb) Get(idx int) *blockData {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	return s.blockDataMap[idx]
}

func (s *BlockDataToMemdb) GetBestBlock() *blockData {
	return s.Get(s.Height)
}

func (s *BlockDataToMemdb) GetSummary(idx int) *apitypes.BlockDataBasic {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	return s.blockSummaryMap[idx]
}

func (s *BlockDataToMemdb) GetBestBlockSummary() *apitypes.BlockDataBasic {
	return s.GetSummary(s.Height)
}

// Store writes blockData to stdout in JSON format
func (s *BlockDataToJSONStdOut) Store(data *blockData) error {
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
	fmt.Printf("\n--- BEGIN blockData JSON ---\n")
	_, err = writeFormattedJSONBlockData(jsonConcat, os.Stdout)
	fmt.Printf("--- END blockData JSON ---\n\n")

	return err
}

// Store writes blockData to stdout as plain text summary
func (s *BlockDataToSummaryStdOut) Store(data *blockData) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	winSize := activeNet.StakeDiffWindowSize

	fmt.Printf("\nBlock %v:\n", data.header.Height)

	fmt.Printf("  Stake difficulty:                 %9.3f -> %.3f (current -> next block)\n",
		data.currentstakediff.CurrentStakeDifficulty,
		data.currentstakediff.NextStakeDifficulty)

	fmt.Printf("  Estimated price in next window:   %9.3f / [%.2f, %.2f] ([min, max])\n",
		data.eststakediff.Expected, data.eststakediff.Min, data.eststakediff.Max)
	fmt.Printf("  Window progress:   %3d / %3d in price window number %v\n",
		data.idxBlockInWindow, winSize, data.priceWindowNum)

	fmt.Printf("  Ticket fees:  %.4f, %.4f, %.4f (mean, median, std), n=%d\n",
		data.feeinfo.Mean, data.feeinfo.Median, data.feeinfo.StdDev,
		data.feeinfo.Number)

	if data.poolinfo.PoolValue >= 0 {
		fmt.Printf("  Ticket pool:  %v (size), %.3f (avg. price), %.2f (total DCR locked)\n",
			data.poolinfo.PoolSize, data.poolinfo.PoolValAvg, data.poolinfo.PoolValue)
	}

	fmt.Printf("  Node connections:  %d\n", data.connections)

	return nil
}

// Store writes blockData to a file in JSON format
// The file name is nameBase+height+".json".
func (s *BlockDataToJSONFiles) Store(data *blockData) error {
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
	height := data.header.Height
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
func JSONFormatBlockData(data *blockData) (*bytes.Buffer, error) {
	var jsonAll bytes.Buffer

	jsonAll.WriteString("{\"estimatestakediff\": ")
	stakeDiffEstJSON, err := json.Marshal(data.eststakediff)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(stakeDiffEstJSON)
	//stakeDiffEstJSON, err := json.MarshalIndent(data.eststakediff, "", "    ")
	//fmt.Println(string(stakeDiffEstJSON))

	jsonAll.WriteString(",\"currentstakediff\": ")
	stakeDiffJSON, err := json.Marshal(data.currentstakediff)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(stakeDiffJSON)

	jsonAll.WriteString(",\"ticketfeeinfo_block\": ")
	feeInfoJSON, err := json.Marshal(data.feeinfo)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(feeInfoJSON)

	jsonAll.WriteString(",\"block_header\": ")
	blockHeaderJSON, err := json.Marshal(data.header)
	if err != nil {
		return nil, err
	}
	jsonAll.Write(blockHeaderJSON)

	jsonAll.WriteString(",\"ticket_pool_info\": ")
	poolInfoJSON, err := json.Marshal(data.poolinfo)
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
