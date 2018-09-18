package dbtypes

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v3/txhelpers"
	"github.com/saintfish/chardet"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"
)

// MsgBlockToDBBlock creates a dbtypes.Block from a wire.MsgBlock
func MsgBlockToDBBlock(msgBlock *wire.MsgBlock, chainParams *chaincfg.Params) *Block {
	// Create the dbtypes.Block structure
	blockHeader := msgBlock.Header

	// convert each transaction hash to a hex string
	var txHashStrs []string
	txHashes := msgBlock.TxHashes()
	for i := range txHashes {
		txHashStrs = append(txHashStrs, txHashes[i].String())
	}

	var stxHashStrs []string
	stxHashes := msgBlock.STxHashes()
	for i := range stxHashes {
		stxHashStrs = append(stxHashStrs, stxHashes[i].String())
	}

	// Assemble the block
	return &Block{
		Hash:       blockHeader.BlockHash().String(),
		Size:       uint32(msgBlock.SerializeSize()),
		Height:     blockHeader.Height,
		Version:    uint32(blockHeader.Version),
		MerkleRoot: blockHeader.MerkleRoot.String(),
		StakeRoot:  blockHeader.StakeRoot.String(),
		NumTx:      uint32(len(msgBlock.Transactions) + len(msgBlock.STransactions)),
		// nil []int64 for TxDbIDs
		NumRegTx:     uint32(len(msgBlock.Transactions)),
		Tx:           txHashStrs,
		NumStakeTx:   uint32(len(msgBlock.STransactions)),
		STx:          stxHashStrs,
		Time:         uint64(blockHeader.Timestamp.Unix()),
		Nonce:        uint64(blockHeader.Nonce),
		VoteBits:     blockHeader.VoteBits,
		FinalState:   blockHeader.FinalState[:],
		Voters:       blockHeader.Voters,
		FreshStake:   blockHeader.FreshStake,
		Revocations:  blockHeader.Revocations,
		PoolSize:     blockHeader.PoolSize,
		Bits:         blockHeader.Bits,
		SBits:        uint64(blockHeader.SBits),
		Difficulty:   txhelpers.GetDifficultyRatio(blockHeader.Bits, chainParams),
		ExtraData:    blockHeader.ExtraData[:],
		StakeVersion: blockHeader.StakeVersion,
		PreviousHash: blockHeader.PrevBlock.String(),
	}
}

// ChartGroupingToInterval converts the chartGrouping value to an actual time
// interval based on the gregorian calendar. AllChartGrouping returns 1 while
// the unknown chartGrouping returns -1 and an error. All the other time
// interval values is returned in terms of seconds.
func ChartGroupingToInterval(grouping ChartGrouping) (float64, error) {
	var hr = 3600.0
	switch grouping {
	case AllChartGrouping:
		return 1, nil

	case DayChartGrouping:
		return hr * 24, nil

	case WeekChartGrouping:
		return hr * 24 * 7, nil

	case MonthChartGrouping:
		return hr * 24 * 30.436875, nil

	case YearChartGrouping:
		return hr * 24 * 30.436875 * 12, nil

	default:
		return -1, fmt.Errorf(`unknown chart grouping "%d"`, grouping)
	}
}

// DetectCharset first extracts data as bytes from the provided hex string.
// From the extracted bytes; it establishes the possible encoding used,
// Language, applicable charset and confidence value in percentage by how much
// the system expects the detected values to be true. Its also returns the
// extracted data in bytes.
func DetectCharset(hexString string) (*CharsetData, error) {
	if len(hexString) == 0 {
		return nil, fmt.Errorf("hex string to be decoded should not be empty")
	}

	charSet := new(CharsetData)
	v := []byte(hexString)

	dst := make([]byte, hex.DecodedLen(len(v)))
	_, err := hex.Decode(dst, v)
	if err != nil {
		return nil, fmt.Errorf("converting hex to bytes failed: %v", err)
	}

	charSet.Data = dst
	charSet.HexStr = hexString
	charSet.contentType = http.DetectContentType(dst)

	result, err := chardet.NewTextDetector().DetectBest(dst)
	if err != nil {
		// do not quit if an error occurs
		log.Printf("detecting charset data failed: %v", err)
	}

	charSet.charset = strings.ToLower(result.Charset)
	charSet.language = result.Language
	charSet.confidence = uint32(result.Confidence)

	return charSet, nil
}

// Decode tries to resolve the bytes in the respective formats into more meaningful
// textual data.
func (chars *CharsetData) Decode() (*CharsetData, error) {
	if len(chars.HexStr) == 0 {
		return nil, fmt.Errorf("hex string to be decoded should not be empty")
	}

	// Do not implement further decoding; if no data in bytes is available,
	// charset was not detected and the confidence value is lest than 25%.
	// 25% is just an estimate of the current acceptable confidence that
	// ensures that further decoding MAY return acceptable data.
	if len(chars.Data) == 0 || len(chars.charset) == 0 || chars.confidence < 25 {
		return chars, nil
	}

	// Works for data in ISO-8859-1 and UTF-8 charset format
	if chars.contentType == "text/plain; charset=utf-8" {
		chars.HexStr = string(chars.Data)
		return chars, nil
	}

	encodingFormat := (encoding.Encoding)(nil)

	// handle windows charset encoding
	switch chars.charset {
	case "windows-1252":
		encodingFormat = charmap.Windows1252
	case "windows-1253":
		encodingFormat = charmap.Windows1253
	case "windows-1254":
		encodingFormat = charmap.Windows1254
	case "windows-1255":
		encodingFormat = charmap.Windows1255
	case "windows-1256":
	case "shift_jis", "shift-jis":
		encodingFormat = japanese.ShiftJIS
	case "gb-18030", "gb18030":
		encodingFormat = simplifiedchinese.GB18030
	case "gbk":
		encodingFormat = simplifiedchinese.GBK
	case "big5":
		encodingFormat = traditionalchinese.Big5
	case "euc-kr":
		encodingFormat = korean.EUCKR
	case "euc-jp":
		encodingFormat = japanese.EUCJP
	case "iso-8859-1":
		encodingFormat = charmap.ISO8859_1
	case "iso-8859-2":
		encodingFormat = charmap.ISO8859_2
	case "iso-8859-5":
		encodingFormat = charmap.ISO8859_5
	case "iso-8859-6":
		encodingFormat = charmap.ISO8859_6
	case "iso-8859-7":
		encodingFormat = charmap.ISO8859_7
	case "iso-8859-8":
		encodingFormat = charmap.ISO8859_8
	case "iso-8859-9":
		encodingFormat = charmap.ISO8859_9
	case "koi8-r":
		encodingFormat = charmap.KOI8R
	case "utf-32be":
		encodingFormat = utf32.All[1]
	case "utf-32le":
		encodingFormat = utf32.All[2]
	case "utf-16be":
		encodingFormat = unicode.All[1]
	case "utf-16le":
		encodingFormat = unicode.All[2]
	default:
		return nil, fmt.Errorf("charset found: (%s) is not yet supported", chars.charset)
	}

	decodedStr, err := transformEncoding(bytes.NewReader(chars.Data), encodingFormat.NewDecoder())
	if err == nil {
		chars.HexStr = decodedStr
	} else {
		log.Printf("failed to decode the text: error %v", err)
	}

	return nil, nil
}

func transformEncoding(rawReader io.Reader, trans transform.Transformer) (string, error) {
	ret, err := ioutil.ReadAll(transform.NewReader(rawReader, trans))
	if err == nil {
		return string(ret), nil
	} else {
		return "", err
	}
}
