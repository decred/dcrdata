package txhelpers

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/wire"
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

	fibExpected := chainjson.FeeInfoBlock{
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

	fibExpected := chainjson.FeeInfoBlock{
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

/*
// ConnectNodeRPC attempts to create a new websocket connection to a dcrd node,
// with the given credentials and optional notification handlers.
func ConnectNodeRPC(host, user, pass, cert string, disableTLS bool) (*rpcclient.Client, semver.Semver, error) {
	var dcrdCerts []byte
	var err error
	var nodeVer semver.Semver
	if !disableTLS {
		dcrdCerts, err = os.ReadFile(cert)
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
	ver, err := dcrdClient.Version(context.TODO())
	if err != nil {
		return nil, nodeVer, fmt.Errorf("unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver.NewSemver(dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch)

	return dcrdClient, nodeVer, nil
}
*/

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

func TestGenesisTxHash(t *testing.T) {
	// Mainnet
	genesisTxHash := GenesisTxHash(chaincfg.MainNetParams()).String()
	if genesisTxHash == "" {
		t.Errorf("Failed to get genesis transaction hash for mainnet.")
	}
	t.Logf("Genesis transaction hash (mainnet): %s", genesisTxHash)

	mainnetExpectedTxHash := "e7dfbceac9fccd6025c70a1dfa9302b3e7b5aa22fa51c98a69164ad403d60a2c"
	if genesisTxHash != mainnetExpectedTxHash {
		t.Errorf("Incorrect genesis transaction hash (mainnet). Expected %s, got %s",
			mainnetExpectedTxHash, genesisTxHash)
	}

	// Simnet
	genesisTxHash = GenesisTxHash(chaincfg.SimNetParams()).String()
	if genesisTxHash == "" {
		t.Errorf("Failed to get genesis transaction hash for simnet.")
	}
	t.Logf("Genesis transaction hash (mainnet): %s", genesisTxHash)

	simnetExpectedTxHash := "a216ea043f0d481a072424af646787794c32bcefd3ed181a090319bbf8a37105"
	if genesisTxHash != simnetExpectedTxHash {
		t.Errorf("Incorrect genesis transaction hash (simnet). Expected %s, got %s",
			mainnetExpectedTxHash, genesisTxHash)
	}
}

func TestAddressErrors(t *testing.T) {
	if AddressErrorNoError != nil {
		t.Errorf("txhelpers.AddressErrorNoError must be <nil>")
	}
}

func TestIsZeroHashP2PHKAddress(t *testing.T) {
	mainnetDummy := "DsQxuVRvS4eaJ42dhQEsCXauMWjvopWgrVg"
	testnetDummy := "TsR28UZRprhgQQhzWns2M6cAwchrNVvbYq2"
	simnetDummy := "SsUMGgvWLcixEeHv3GT4TGYyez4kY79RHth"

	positiveTest := true
	negativeTest := !positiveTest

	testIsZeroHashP2PHKAddress(t, mainnetDummy, chaincfg.MainNetParams(), positiveTest)
	testIsZeroHashP2PHKAddress(t, testnetDummy, chaincfg.TestNet3Params(), positiveTest)
	testIsZeroHashP2PHKAddress(t, simnetDummy, chaincfg.SimNetParams(), positiveTest)

	// wrong network
	testIsZeroHashP2PHKAddress(t, mainnetDummy, chaincfg.SimNetParams(), negativeTest)
	testIsZeroHashP2PHKAddress(t, testnetDummy, chaincfg.MainNetParams(), negativeTest)
	testIsZeroHashP2PHKAddress(t, simnetDummy, chaincfg.TestNet3Params(), negativeTest)

	// wrong address
	testIsZeroHashP2PHKAddress(t, "", chaincfg.SimNetParams(), negativeTest)
	testIsZeroHashP2PHKAddress(t, "", chaincfg.MainNetParams(), negativeTest)
	testIsZeroHashP2PHKAddress(t, "", chaincfg.TestNet3Params(), negativeTest)
}

func testIsZeroHashP2PHKAddress(t *testing.T, expectedAddress string, params *chaincfg.Params, expectedTestResult bool) {
	result := IsZeroHashP2PHKAddress(expectedAddress, params)
	if expectedTestResult != result {
		t.Fatalf("IsZeroHashP2PHKAddress(%v) returned <%v>, expected <%v>",
			expectedAddress, result, expectedTestResult)
	}
}

func TestFeeRate(t *testing.T) {
	// Ensure invalid fee rate is -1.
	if FeeRate(0, 0, 0) != -1 {
		t.Errorf("Fee rate for 0 byte size must return -1.")
	}

	// (1-2)/500*1000 = -2
	expected := int64(-2)
	got := FeeRate(1, 2, 500)
	if got != expected {
		t.Errorf("Expected fee rate of %d, got %d.", expected, got)
	}

	// (10-0)/100*1000 = 100
	expected = int64(100)
	got = FeeRate(10, 0, 100)
	if got != expected {
		t.Errorf("Expected fee rate of %d, got %d.", expected, got)
	}

	// (10-10)/1e9*1000 = 0
	expected = int64(0)
	got = FeeRate(10, 10, 1e9)
	if got != expected {
		t.Errorf("Expected fee rate of %d, got %d.", expected, got)
	}
}

func randomHash() chainhash.Hash {
	var hash chainhash.Hash
	if _, err := rand.Read(hash[:]); err != nil {
		panic("boom")
	}
	return hash
}

func TestIsZeroHash(t *testing.T) {
	tests := []struct {
		name string
		hash chainhash.Hash
		want bool
	}{
		{"correctFromZeroByteArray", [chainhash.HashSize]byte{}, true},
		{"correctFromZeroValueHash", chainhash.Hash{}, true},
		{"incorrectByteArrayValues", [chainhash.HashSize]byte{0x22}, false},
		{"incorrectRandomHash", randomHash(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsZeroHash(tt.hash); got != tt.want {
				t.Errorf("IsZeroHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsZeroHashStr(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want bool
	}{
		{"correctFromStringsRepeat", strings.Repeat("00", chainhash.HashSize), true},
		{"correctFromZeroHashStringer", zeroHash.String(), true},
		{"correctFromZeroValueHashStringer", chainhash.Hash{}.String(), true},
		{"incorrectEmptyString", "", false},
		{"incorrectRandomHashString", randomHash().String(), false},
		{"incorrectNotAHashAtAll", "this is totally not a hash let alone the zero hash string", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsZeroHashStr(tt.hash); got != tt.want {
				t.Errorf("IsZeroHashStr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMsgTxFromHex(t *testing.T) {
	tests := []struct {
		testName string
		txhex    string
		want     *wire.MsgTx
		wantErr  bool
	}{
		{
			testName: "badHex",
			txhex:    "thisainthex",
			want:     nil,
			wantErr:  true, // encoding/hex: invalid byte: U+0074 't'
		},
		{
			testName: "partialTxHex",
			txhex: "0100000002000000000000000000000000000000000000000000000000000000" +
				"0000000000ffffffff00ffffffffcbc2bc0d947d8ebfa22ef060db230e3ecca0" +
				"8caa20e9eedcfc573dea71af2c940000000001ffffffff040000000000000000" +
				"0000266a2464e719ba6f832b4a51caf58a88cd4f6a57789f6cffe5c014000000" +
				"000000000070fd040000000000000000000000086a060100050000006e0d2100" +
				"0000000000001abb76a91414362cb17eb0295c03b051aad6abd87e31bd2fd0",
			want:    nil,
			wantErr: true, // unexpected EOF
		},
		{
			testName: "badTxType",
			txhex:    "0000002000000000000000000000000000000000000000000000000000000",
			want:     nil,
			wantErr:  true, // MsgTx.BtcDecode: unsupported transaction type
		},
		{
			testName: "ok",
			txhex: "0100000002000000000000000000000000000000000000000000000000000000" +
				"0000000000ffffffff00ffffffffcbc2bc0d947d8ebfa22ef060db230e3ecca0" +
				"8caa20e9eedcfc573dea71af2c940000000001ffffffff040000000000000000" +
				"0000266a2464e719ba6f832b4a51caf58a88cd4f6a57789f6cffe5c014000000" +
				"000000000070fd040000000000000000000000086a060100050000006e0d2100" +
				"0000000000001abb76a91414362cb17eb0295c03b051aad6abd87e31bd2fd088" +
				"ac84ee19b10200000000001abb76a914fcc2ec1e801444402b23b204328b32b0" +
				"76e62e6088ac000000000000000002348695060000000000000000ffffffff02" +
				"0000bf75a5aa02000000a6f904000f000000914830450221008abbf185422662" +
				"0727fd0f0a9f5cbad324b223ae5f2708d397f257882b3e116702204b0b048b6d" +
				"6a0c129a8b6bfa0788c7d2ee597fe0f85d6dd5b43d625f55d214290147512102" +
				"3259b72bbb675b34cb0ad519d3f8bc5f58bd0ee4aec8a4d248c204a926c953e2" +
				"2102be3864d8c7264baa1ef75e3fccb1697ab9a403f6e95ff11614a7fb22af3c" +
				"5c0f52ae",
			want: &wire.MsgTx{
				CachedHash: nil,
				SerType:    0,
				Version:    1,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: wire.OutPoint{Hash: zeroHash, Index: 4294967295, Tree: 0},
						Sequence:         4294967295,
						ValueIn:          110462516,
						BlockHeight:      0,
						BlockIndex:       4294967295,
						SignatureScript:  []uint8{00, 00},
					},
					{
						PreviousOutPoint: wire.OutPoint{
							Hash: chainhash.Hash{0xcb, 0xc2, 0xbc, 0xd, 0x94, 0x7d, 0x8e, 0xbf,
								0xa2, 0x2e, 0xf0, 0x60, 0xdb, 0x23, 0xe, 0x3e, 0xcc, 0xa0,
								0x8c, 0xaa, 0x20, 0xe9, 0xee, 0xdc, 0xfc, 0x57, 0x3d,
								0xea, 0x71, 0xaf, 0x2c, 0x94},
							Index: 0,
							Tree:  1,
						},
						Sequence:    4294967295,
						ValueIn:     11452904895,
						BlockHeight: 326054,
						BlockIndex:  15,
						SignatureScript: []byte{0x48, 0x30, 0x45, 0x2, 0x21, 0x0, 0x8a,
							0xbb, 0xf1, 0x85, 0x42, 0x26, 0x62, 0x7, 0x27, 0xfd, 0xf, 0xa,
							0x9f, 0x5c, 0xba, 0xd3, 0x24, 0xb2, 0x23, 0xae, 0x5f, 0x27,
							0x8, 0xd3, 0x97, 0xf2, 0x57, 0x88, 0x2b, 0x3e, 0x11, 0x67,
							0x2, 0x20, 0x4b, 0xb, 0x4, 0x8b, 0x6d, 0x6a, 0xc, 0x12, 0x9a,
							0x8b, 0x6b, 0xfa, 0x7, 0x88, 0xc7, 0xd2, 0xee, 0x59, 0x7f,
							0xe0, 0xf8, 0x5d, 0x6d, 0xd5, 0xb4, 0x3d, 0x62, 0x5f, 0x55,
							0xd2, 0x14, 0x29, 0x1, 0x47, 0x51, 0x21, 0x2, 0x32, 0x59, 0xb7,
							0x2b, 0xbb, 0x67, 0x5b, 0x34, 0xcb, 0xa, 0xd5, 0x19, 0xd3,
							0xf8, 0xbc, 0x5f, 0x58, 0xbd, 0xe, 0xe4, 0xae, 0xc8, 0xa4,
							0xd2, 0x48, 0xc2, 0x4, 0xa9, 0x26, 0xc9, 0x53, 0xe2, 0x21, 0x2,
							0xbe, 0x38, 0x64, 0xd8, 0xc7, 0x26, 0x4b, 0xaa, 0x1e, 0xf7,
							0x5e, 0x3f, 0xcc, 0xb1, 0x69, 0x7a, 0xb9, 0xa4, 0x3, 0xf6,
							0xe9, 0x5f, 0xf1, 0x16, 0x14, 0xa7, 0xfb, 0x22, 0xaf, 0x3c,
							0x5c, 0xf, 0x52, 0xae},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:   0,
						Version: 0,
						PkScript: []byte{0x6a, 0x24, 0x64, 0xe7, 0x19, 0xba, 0x6f, 0x83,
							0x2b, 0x4a, 0x51, 0xca, 0xf5, 0x8a, 0x88, 0xcd, 0x4f, 0x6a,
							0x57, 0x78, 0x9f, 0x6c, 0xff, 0xe5, 0xc0, 0x14, 0x0, 0x0,
							0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x70, 0xfd, 0x4, 0x0},
					},
					{
						Value:    0,
						Version:  0,
						PkScript: []byte{0x6a, 0x6, 0x1, 0x0, 0x5, 0x0, 0x0, 0x0},
					},
					{
						Value:   2166126,
						Version: 0,
						PkScript: []byte{0xbb, 0x76, 0xa9, 0x14, 0x14, 0x36, 0x2c, 0xb1,
							0x7e, 0xb0, 0x29, 0x5c, 0x3, 0xb0, 0x51, 0xaa, 0xd6, 0xab,
							0xd8, 0x7e, 0x31, 0xbd, 0x2f, 0xd0, 0x88, 0xac},
					},
					{
						Value:   11561201284,
						Version: 0,
						PkScript: []byte{0xbb, 0x76, 0xa9, 0x14, 0xfc, 0xc2, 0xec, 0x1e,
							0x80, 0x14, 0x44, 0x40, 0x2b, 0x23, 0xb2, 0x4, 0x32, 0x8b,
							0x32, 0xb0, 0x76, 0xe6, 0x2e, 0x60, 0x88, 0xac},
					},
				},
				LockTime: 0,
				Expiry:   0,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			got, err := MsgTxFromHex(tt.txhex)
			if (err != nil) != tt.wantErr {
				t.Errorf("MsgTxFromHex() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			//t.Log(spew.Sdump(got))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MsgTxFromHex() = %v, want %v", got, tt.want)
			}
			if err != nil {
				t.Log(err)
			}
		})
	}
}

func BenchmarkMsgTxFromHex(b *testing.B) {
	txHex :=
		"0100000002000000000000000000000000000000000000000000000000000000" +
			"0000000000ffffffff00ffffffffcbc2bc0d947d8ebfa22ef060db230e3ecca0" +
			"8caa20e9eedcfc573dea71af2c940000000001ffffffff040000000000000000" +
			"0000266a2464e719ba6f832b4a51caf58a88cd4f6a57789f6cffe5c014000000" +
			"000000000070fd040000000000000000000000086a060100050000006e0d2100" +
			"0000000000001abb76a91414362cb17eb0295c03b051aad6abd87e31bd2fd088" +
			"ac84ee19b10200000000001abb76a914fcc2ec1e801444402b23b204328b32b0" +
			"76e62e6088ac000000000000000002348695060000000000000000ffffffff02" +
			"0000bf75a5aa02000000a6f904000f000000914830450221008abbf185422662" +
			"0727fd0f0a9f5cbad324b223ae5f2708d397f257882b3e116702204b0b048b6d" +
			"6a0c129a8b6bfa0788c7d2ee597fe0f85d6dd5b43d625f55d214290147512102" +
			"3259b72bbb675b34cb0ad519d3f8bc5f58bd0ee4aec8a4d248c204a926c953e2" +
			"2102be3864d8c7264baa1ef75e3fccb1697ab9a403f6e95ff11614a7fb22af3c" +
			"5c0f52ae"

	for i := 0; i < b.N; i++ {
		_, err := MsgTxFromHex(txHex)
		if err != nil {
			b.Fatal(err)
		}
	}
}
