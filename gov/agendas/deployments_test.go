package agendas

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrdata/db/dbtypes/v2"
)

var db *storm.DB
var tempDir string

var firstAgendaInfo = &AgendaTagged{
	ID:          "fixlnseqlocks",
	Description: "Modify sequence lock handling as defined in DCP0004",
	Mask:        6,
	StartTime:   1548633600,
	ExpireTime:  1580169600,
	Status:      dbtypes.InitialAgendaStatus,
	Choices: []chainjson.Choice{
		{
			ID:          "abstain",
			Description: "abstain voting for change",
			IsAbstain:   true,
		}, {
			ID:          "no",
			Description: "keep the existing consensus rules",
			Bits:        2,
			IsNo:        true,
		}, {
			ID:          "yes",
			Description: "change to the new consensus rules",
			Bits:        4,
		},
	},
	VoteVersion: 4,
}

// TestMain sets up the temporary db needed for testing
func TestMain(m *testing.M) {
	var err error
	tempDir, err = ioutil.TempDir(os.TempDir(), "onchain")
	if err != nil {
		panic(err)
	}

	db, err = storm.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		panic(err)
	}

	//  Save the first sample agendaInfo
	err = db.Save(firstAgendaInfo)
	if err != nil {
		panic(err)
	}

	returnVal := m.Run()

	db.Close()
	// clean up
	os.RemoveAll(tempDir)

	// Exit with the return value
	os.Exit(returnVal)
}

// testClient needed to mock the actual client GetVoteInfo implementation
type testClient int

// GetVoteInfo implementation showing a sample data format expected.
func (*testClient) GetVoteInfo(version uint32) (*chainjson.GetVoteInfoResult, error) {
	if version != 5 {
		return &chainjson.GetVoteInfoResult{}, fmt.Errorf(voteVersionErrMsg, version)
	}
	resp := &chainjson.GetVoteInfoResult{
		CurrentHeight: 319842,
		StartHeight:   318592,
		EndHeight:     326655,
		Hash:          "000000000000000021a2128e44824adc7ad0560d38ca0692aeb8281f75b6d9a0",
		VoteVersion:   5,
		Quorum:        4032,
		TotalVotes:    0,
		Agendas: []chainjson.Agenda{
			{
				ID:             "TestAgenda0001",
				Description:    "This agenda just shows chainjson.GetVoteInfoResult payload format",
				Mask:           6,
				StartTime:      1493164800,
				ExpireTime:     1524700800,
				Status:         "active",
				QuorumProgress: 100,
				Choices: []chainjson.Choice{
					{
						ID:          "abstain",
						Description: "abstain voting for change",
						Bits:        0,
						IsAbstain:   true,
						IsNo:        false,
						Count:       120,
						Progress:    100,
					},
					{
						ID:          "no",
						Description: "keep the existing algorithm",
						Bits:        2,
						IsAbstain:   false,
						IsNo:        true,
						Count:       10,
						Progress:    100,
					},
					{
						ID:          "yes",
						Description: "change to the new algorithm",
						Bits:        4,
						IsAbstain:   false,
						IsNo:        false,
						Count:       12120,
						Progress:    100,
					},
				},
			},
		},
	}
	return resp, nil
}

// TestNewAgendasDB tests the functionality of NewAgendaDB.
func TestNewAgendasDB(t *testing.T) {
	type testData struct {
		rpc    DeploymentSource
		dbPath string
		// Outputs
		IsDBInstance bool
		errMsg       string
	}

	var client *testClient
	testPath := filepath.Join(tempDir, "test2.db")

	td := []testData{
		{nil, "", false, "empty db Path found"},
		{client, "", false, "empty db Path found"},
		{nil, testPath, false, "invalid deployment source found"},
		{client, testPath, true, ""},
	}

	for _, val := range td {
		results, err := NewAgendasDB(val.rpc, val.dbPath)
		if err == nil && val.errMsg != "" {
			t.Fatalf("expected no error but found '%v' ", err)
		}

		if err != nil && err.Error() != val.errMsg {
			t.Fatalf(" expected error '%v' but found '%v", val.errMsg, err)
		}

		if results == nil && val.IsDBInstance {
			t.Fatal("expected a non-nil db instance but found a nil instance")
		}

		// If a valid db instance is expected test if the corresponding db exists.
		if val.IsDBInstance {
			if _, err := os.Stat(val.dbPath); os.IsNotExist(err) {
				t.Fatalf("expected to find the corresponding db at '%v' path but did not.", val.dbPath)
			}
		}
	}
}

// voteVersionErrMsg is a sample error message that does not imply the format of
// the actual error rpcclient.GetVoteInfo returns when an invalid votes version
// is provided.
var voteVersionErrMsg = "invalid vote version %d found"

var expectedAgenda = &AgendaTagged{
	ID:             "TestAgenda0001",
	Description:    "This agenda just shows chainjson.GetVoteInfoResult payload format",
	Mask:           6,
	StartTime:      1493164800,
	ExpireTime:     1524700800,
	Status:         dbtypes.ActivatedAgendaStatus,
	QuorumProgress: 100,
	Choices: []chainjson.Choice{
		{
			ID:          "abstain",
			Description: "abstain voting for change",
			IsAbstain:   true,
			Count:       120,
			Progress:    100,
		},
		{
			ID:          "no",
			Description: "keep the existing algorithm",
			Bits:        2,
			IsNo:        true,
			Count:       10,
			Progress:    100,
		},
		{
			ID:          "yes",
			Description: "change to the new algorithm",
			Bits:        4,
			Count:       12120,
			Progress:    100,
		},
	},
	VoteVersion: 5,
}

var activeVersions = map[uint32][]chaincfg.ConsensusDeployment{
	5: {
		{
			Vote: chaincfg.Vote{
				Id:          "TestAgenda0001",
				Description: "This agenda just shows chainjson.GetVoteInfoResult payload format",
				Mask:        6,
				Choices: []chaincfg.Choice{
					{
						Id:          "abstain",
						Description: "abstain voting for change",
						Bits:        0,
						IsAbstain:   true,
						IsNo:        false,
					},
					{
						Id:          "no",
						Description: "keep the existing consensus rules",
						Bits:        2,
						IsAbstain:   false,
						IsNo:        true,
					},
					{
						Id:          "yes",
						Description: "change to the new consensus rules",
						Bits:        4,
						IsAbstain:   false,
						IsNo:        false,
					},
				},
			},
			StartTime:  1548633600,
			ExpireTime: 1580169600,
		},
	},
}

// TestUpdateAndRetrievals tests the agendas db updating and retrieval of one
// and many agendas.
func TestUpdateAndRetrievals(t *testing.T) {
	var client *testClient

	dbInstance := &AgendaDB{sdb: db, rpcClient: client}
	invalidVersions := map[uint32][]chaincfg.ConsensusDeployment{20: {}}

	type testData struct {
		db           *AgendaDB
		voteVersions map[uint32][]chaincfg.ConsensusDeployment
		errMsg       string
	}

	td := []testData{
		{nil, nil, "AgendaDB was not initialized correctly"},
		{&AgendaDB{}, nil, "AgendaDB was not initialized correctly"},
		{dbInstance, activeVersions, ""},
		{dbInstance, invalidVersions, `agendas.CheckAgendasUpdates failed: vote ` +
			`version 20 agendas retrieval failed: invalid vote version 20 found`},
	}

	// Test saving updates to agendas db.
	for i, val := range td {
		t.Run("Test_CheckAgendasUpdates_#"+strconv.Itoa(i), func(t *testing.T) {
			err := val.db.CheckAgendasUpdates(val.voteVersions)
			if err != nil && val.errMsg != err.Error() {
				t.Fatalf("expect to find error '%s' but found '%v' ", val.errMsg, err)
			}

			if err == nil && val.errMsg != "" {
				t.Fatalf("expected to find error '%s' but none was returned ", val.errMsg)
			}

			if err == nil && val.db.NumAgendas != 2 {
				t.Fatalf("expected to find 2 agendas added but found '%d'", val.db.NumAgendas)
			}
		})
	}

	// Test retrieval of all agendas.
	t.Run("Test_AllAgendas", func(t *testing.T) {
		agendas, err := dbInstance.AllAgendas()
		if err != nil {
			t.Fatalf("expected no error but found '%v'", err)
		}

		if len(agendas) != 2 {
			t.Fatalf("expected to find two agendas but found: %d ", len(agendas))
		}

		for _, data := range agendas {
			if data == nil {
				t.Fatal("expected to find non nil data")
			}

			switch data.ID {
			case firstAgendaInfo.ID:
				if !reflect.DeepEqual(data, firstAgendaInfo) {
					t.Fatal("expected the returned data to be equal to firstAgendainfo but it wasn't")
				}

			case expectedAgenda.ID:
				if !reflect.DeepEqual(data, expectedAgenda) {
					t.Fatal("expected the returned data to be equal to second agenda data but it wasn't")
				}
			default:
				t.Fatalf("inaccurate agendas data found")
			}
		}
	})

	// Testing retrieval of single agenda by ID.
	t.Run("Test_AgendaInfo", func(t *testing.T) {
		agenda, err := dbInstance.AgendaInfo(firstAgendaInfo.ID)
		if err != nil {
			t.Fatalf("expected to find no error but found %v", err)
		}

		if !reflect.DeepEqual(agenda, firstAgendaInfo) {
			t.Fatal("expected the agenda info returned to be equal to firstAgendaInfo but it wasn't")
		}
	})
}
