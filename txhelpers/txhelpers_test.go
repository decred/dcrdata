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
	"github.com/decred/dcrdata/semver"
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
