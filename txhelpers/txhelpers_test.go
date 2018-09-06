package txhelpers

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrdata/v3/semver"
)

type TxGetter struct {
	txLookup map[chainhash.Hash]*dcrutil.Tx
}

func (t TxGetter) GetRawTransaction(txHash *chainhash.Hash) (*dcrutil.Tx, error) {
	tx, ok := t.txLookup[*txHash]
	var err error
	if !ok {
		err = fmt.Errorf("tx not found")
	}
	return tx, err
}

func LoadTestBlockAndSSTX(t *testing.T) (*dcrutil.Block, []*dcrutil.Tx) {
	// Load block data
	blockTestFileName := "block138883.bin"
	blockTestFile, err := os.Open(blockTestFileName)
	if err != nil {
		t.Fatalf("Unable to open file %s: %v", blockTestFileName, err)
	}
	defer blockTestFile.Close()
	block, err := dcrutil.NewBlockFromReader(blockTestFile)
	if err != nil {
		t.Fatalf("Unable to load test block data.")
	}
	t.Logf("Loaded block %d", block.Height())

	// Load SSTX data
	blockTestSSTXFileName := "block138883sstx.bin"
	txFile, err := os.Open(blockTestSSTXFileName)
	if err != nil {
		t.Fatalf("Unable to open file %s: %v", blockTestSSTXFileName, err)
	}
	defer txFile.Close()

	var numSSTX uint32
	if err = binary.Read(txFile, binary.LittleEndian, &numSSTX); err != nil {
		t.Fatalf("Unable to read file %s: %v", blockTestSSTXFileName, err)
	}

	allTxRead := make([]*dcrutil.Tx, numSSTX)
	for i := range allTxRead {
		var txSize int64
		if err = binary.Read(txFile, binary.LittleEndian, &txSize); err != nil {
			t.Fatalf("Unable to read file %s: %v", blockTestSSTXFileName, err)
		}

		allTxRead[i], err = dcrutil.NewTxFromReader(txFile)
		if err != nil {
			t.Fatal(err)
		}

		var txTree int8
		if err = binary.Read(txFile, binary.LittleEndian, &txTree); err != nil {
			t.Fatalf("Unable to read file %s: %v", blockTestSSTXFileName, err)
		}
		allTxRead[i].SetTree(txTree)

		var txIndex int64
		if err = binary.Read(txFile, binary.LittleEndian, &txIndex); err != nil {
			t.Fatalf("Unable to read file %s: %v", blockTestSSTXFileName, err)
		}
		allTxRead[i].SetIndex(int(txIndex))
	}

	t.Logf("Read %d SSTX", numSSTX)

	return block, allTxRead
}

func TestFeeRateInfoBlock(t *testing.T) {
	block, _ := LoadTestBlockAndSSTX(t)

	fib := FeeRateInfoBlock(block)
	t.Log(*fib)

	fibExpected := dcrjson.FeeInfoBlock{
		Height: 138883,
		Number: 20,
		Min:    0.5786178114478114,
		Max:    0.70106,
		Mean:   0.5969256371196103,
		Median: 0.595365723905724,
		StdDev: 0.02656563242880357,
	}

	if !reflect.DeepEqual(fibExpected, *fib) {
		t.Errorf("Fee Info Block mismatch. Expected %v, got %v.", fibExpected, *fib)
	}
}

func TestFeeInfoBlock(t *testing.T) {
	block, _ := LoadTestBlockAndSSTX(t)

	fib := FeeInfoBlock(block)
	t.Log(*fib)

	fibExpected := dcrjson.FeeInfoBlock{
		Height: 138883,
		Number: 20,
		Min:    0.17184949,
		Max:    0.3785724,
		Mean:   0.21492538949999998,
		Median: 0.17682362,
		StdDev: 0.07270582117405575,
	}

	if !reflect.DeepEqual(fibExpected, *fib) {
		t.Errorf("Fee Info Block mismatch. Expected %v, got %v.", fibExpected, *fib)
	}
}

// Utilities for creating test data:

func TxToWriter(tx *dcrutil.Tx, w io.Writer) error {
	msgTx := tx.MsgTx()
	binary.Write(w, binary.LittleEndian, int64(msgTx.SerializeSize()))
	msgTx.Serialize(w)
	binary.Write(w, binary.LittleEndian, tx.Tree())
	binary.Write(w, binary.LittleEndian, int64(tx.Index()))
	return nil
}

// ConnectNodeRPC attempts to create a new websocket connection to a dcrd node,
// with the given credentials and optional notification handlers.
func ConnectNodeRPC(host, user, pass, cert string, disableTLS bool) (*rpcclient.Client, semver.Semver, error) {
	var dcrdCerts []byte
	var err error
	var nodeVer semver.Semver
	if !disableTLS {
		dcrdCerts, err = ioutil.ReadFile(cert)
		if err != nil {
			return nil, nodeVer, err
		}

	}

	connCfgDaemon := &rpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws", // websocket
		User:         user,
		Pass:         pass,
		Certificates: dcrdCerts,
		DisableTLS:   disableTLS,
	}

	dcrdClient, err := rpcclient.New(connCfgDaemon, nil)
	if err != nil {
		return nil, nodeVer, fmt.Errorf("Failed to start dcrd RPC client: %s", err.Error())
	}

	// Ensure the RPC server has a compatible API version.
	ver, err := dcrdClient.Version()
	if err != nil {
		return nil, nodeVer, fmt.Errorf("unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver.NewSemver(dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch)

	return dcrdClient, nodeVer, nil
}

func TestFilterHashSlice(t *testing.T) {
	var hashList, blackList []chainhash.Hash
	var h *chainhash.Hash

	h, _ = chainhash.NewHashFromStr("8e5b17d75d1845f90940d07ac8338d0919f1cbd8e12e943c972322c628b47416")
	hashList = append(hashList, *h)
	h, _ = chainhash.NewHashFromStr("3365991083571c527bd3c81bd7374b6f06c17e67b50671067e78371e0511d1d5") // ***
	hashList = append(hashList, *h)
	h, _ = chainhash.NewHashFromStr("fd1a252947ee2ba7be5d0b197952640bdd74066a2a36f3c00beca34dbd3ac8ad")
	hashList = append(hashList, *h)

	h, _ = chainhash.NewHashFromStr("7ea06b193187dc028b6266ce49f4c942b3d57b4572991b527b5abd9ade4974b8")
	blackList = append(blackList, *h)
	h, _ = chainhash.NewHashFromStr("0839e25863e4d04b099d945d57180283e8be217ce6d7bc589c289bc8a1300804")
	blackList = append(blackList, *h)
	h, _ = chainhash.NewHashFromStr("3365991083571c527bd3c81bd7374b6f06c17e67b50671067e78371e0511d1d5") // *** [2]
	blackList = append(blackList, *h)
	h, _ = chainhash.NewHashFromStr("3edbc5318c36049d5fa70e6b04ef69b02d68e98c4739390c50220509a9803e26")
	blackList = append(blackList, *h)
	h, _ = chainhash.NewHashFromStr("37e032ece5ef4bda7b86c8b410476f3399d1ab48863d7d6279a66bea1e3876ab")
	blackList = append(blackList, *h)

	t.Logf("original: %v", hashList)

	hashList = FilterHashSlice(hashList, func(h chainhash.Hash) bool {
		return HashInSlice(h, blackList)
	})

	t.Logf("filtered: %v", hashList)

	if HashInSlice(blackList[2], hashList) {
		t.Errorf("filtered slice still has hash %v", blackList[2])
	}
}
