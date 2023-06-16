package agendas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/asdine/storm/v3"
	"github.com/decred/dcrd/dcrjson/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrdata/v8/db/dbtypes"
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
	tempDir, err = os.MkdirTemp("", "onchain")
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
func (*testClient) GetVoteInfo(_ context.Context, version uint32) (*chainjson.GetVoteInfoResult, error) {
	if version != 5 {
		msg := fmt.Sprintf("stake version %d does not exist", version)
		return nil, dcrjson.NewRPCError(dcrjson.ErrRPCInvalidParameter, msg)
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

	for i, val := range td {
		results, err := NewAgendasDB(val.rpc, val.dbPath)
		if err == nil && val.errMsg != "" {
			t.Fatalf("expected no error but found '%v' ", err)
		}

		if err != nil && err.Error() != val.errMsg {
			t.Fatalf(" expected error '%v' but found '%v", val.errMsg, err)
		}

		if results == nil && val.IsDBInstance {
			t.Fatalf("%d: expected a non-nil db instance but found a nil instance", i)
		}

		// If a valid db instance is expected test if the corresponding db exists.
		if val.IsDBInstance {
			if _, err := os.Stat(val.dbPath); os.IsNotExist(err) {
				t.Fatalf("expected to find the corresponding db at '%v' path but did not.", val.dbPath)
			}
		}
	}
}

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

// TestUpdateAndRetrievals tests the agendas db updating and retrieval of one
// and many agendas.
func TestUpdateAndRetrievals(t *testing.T) {
	var client *testClient

	voteVersions := []uint32{5}
	dbInstance := &AgendaDB{
		sdb:           db,
		deploySource:  client,
		stakeVersions: voteVersions,
	}

	err := dbInstance.UpdateAgendas()
	if err != nil {
		t.Fatalf("agenda update error: %v", err)
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
