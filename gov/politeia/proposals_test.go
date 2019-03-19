package politeia

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/asdine/storm"
	pitypes "github.com/decred/dcrdata/v4/gov/politeia/types"
	piapi "github.com/decred/politeia/politeiawww/api/v1"
)

var db *storm.DB
var tempDir string

// initial sample proposal made.
var firstProposal = &pitypes.ProposalInfo{
	Name:      "Initial Test proposal",
	State:     2,
	Status:    4,
	Timestamp: 1541904469,
	UserID:    "18b24b6c-14a8-45f6-ab2e-a34127840fb3",
	Username:  "secret-coder",
	PublicKey: "c7580e9d13a21a2046557f7ef0148a5be89fbe8db8c",
	Signature: "8a1b69eb08b413b3ad3161c9b43b6a65a25c537f6151866d391a352",
	Version:   "6",
	Censorship: pitypes.CensorshipRecord{
		Token:      "0aaab331075d08cb03333d5a1bef04b99a708dcbfebc8f8c94040ceb1676e684",
		MerkleRoot: "cfaf772010b439db2fa175b407f7c61fc7b06fbd844192a89551abe40791b6bb",
		Signature:  "6f8a7740c518972c4dc607e877afc794be9f99a1c4790837a7104b7eb6228d4db219",
	},
	NumComments:   23,
	PublishedDate: 1541904469,
	AbandonedDate: 1543946266,
}

// TestMain sets up the temporary db needed for testing
func TestMain(m *testing.M) {
	var err error
	tempDir, err = ioutil.TempDir("", "offchain")
	if err != nil {
		log.Error(err)
		return
	}

	db, err = storm.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		log.Error(err)
		return
	}

	//  Save the first sample proposal
	err = db.Save(firstProposal)
	if err != nil {
		log.Error(err)
		return
	}

	returnVal := m.Run()

	defer db.Close()
	defer os.RemoveAll(tempDir) // clean up

	os.Exit(returnVal)
}

// TestNewProposalsDB tests creating a new storm db and a http client instance.
func TestNewProposalsDB(t *testing.T) {
	var count int
	inputURLPath := "https://proposals.decred.org"
	expectedPath := "https://proposals.decred.org/api/v1"
	getDbPath := func() string {
		count++
		return filepath.Join(tempDir, fmt.Sprintf("test%v.db", count))
	}

	type testData struct {
		politeiaAPIURL string
		dbPath         string

		// Checks if the db was created and its instance referenced returned.
		IsdbInstance bool
		errMsg       string
	}

	td := []testData{
		{
			politeiaAPIURL: "",
			dbPath:         "",
			IsdbInstance:   false,
			errMsg:         "missing politeia API URL",
		},
		{
			politeiaAPIURL: inputURLPath,
			dbPath:         "",
			IsdbInstance:   false,
			errMsg:         "missing db path",
		},
		{
			politeiaAPIURL: "",
			dbPath:         getDbPath(),
			IsdbInstance:   false,
			errMsg:         "missing politeia API URL",
		},
		{
			politeiaAPIURL: inputURLPath,
			dbPath:         getDbPath(),
			IsdbInstance:   true,
			errMsg:         "",
		},
	}

	for i, data := range td {
		t.Run("Test_#"+strconv.Itoa(i), func(t *testing.T) {
			result, err := NewProposalsDB(data.politeiaAPIURL, data.dbPath)

			var expectedErrMsg string
			if err != nil {
				expectedErrMsg = err.Error()
			}

			if expectedErrMsg != data.errMsg {
				t.Fatalf("expected to find error '%v' but found '%v'", data.errMsg, err)
			}

			if data.IsdbInstance && result != nil {
				if result.APIURLpath != expectedPath {
					t.Fatalf("expected the API URL to '%v' but found '%v'", result.APIURLpath, expectedPath)
				}

				if result.client == nil {
					t.Fatal("expected the http client not to be nil but was nil")
				}

				if result.dbP == nil {
					t.Fatal("expected the db instance not to be nil but was nil")
				}
			} else if result != nil {
				// The result should be nil since the incorrect inputs resulted
				// to an error being returned and a nil proposalDB instance.
				t.Fatalf("expect the returned result to be nil but was not nil")
			}

			// expects to find the correspnding db at the given path.
			if data.IsdbInstance {
				if _, err := os.Stat(data.dbPath); os.IsNotExist(err) {
					t.Fatalf("expected to find the corresponding db at '%v' path but did not.", data.dbPath)
				}
			}
		})
	}
}

// mockServer mocks helps avoid making actual http calls during tests. It payloads
// in the same format as would be returned by the normal API endpoint.
func mockServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp string
		switch r.URL.Path {
		case piapi.RouteAllVetted:
			resp = `{  
			"proposals":[  
			   {  
				  "name":"Change language: PoS Mining to PoS Voting, Stakepool to Voting Service Provider",
				  "state":2,
				  "status":4,
				  "timestamp":1539880429,
				  "userid":"350a4b6c-5cdd-4d87-822a-4900dc3a930c",
				  "username":"richard-red",
				  "publickey":"cd6e57b93f95dd0386d670c7ce42cb0ccd1cd5b997e87a716e9359e20251994e",
				  "signature":"c0e3285d447fd2acf1f2e1a0c86a71383dfe71b1b01e0068e56e8e7649dadb7aa503a5f99765fc3a24da8716fd5b89f75bb97762e756f15303e96d135a2e7109",
				  "files":[  
		 
				  ],
				  "numcomments":19,
				  "version":"1",
				  "publishedat":1539898457,
				  "censorshiprecord":{  
					 "token":"522652954ea7998f3fca95b9c4ca8907820eb785877dcf7fba92307131818c75",
					 "merkle":"20c9234c50e0dc78d28003fd57995192a16ca73349f5d97be456128984e463fc",
					 "signature":"d1d44788cdf8d838aad97aa829b2f27f8a32897010d6373c9d3ca1a42820dcafe2615c1904558c6628c5f9165691ead087c0cb2ada023b9aa3f76b6c587ac90e"
				  }
			   }
			]
		 }`
		case piapi.RouteAllVoteStatus:
			resp = `{  
		   "votesstatus":[  
			  {  
				 "token":"522652954ea7998f3fca95b9c4ca8907820eb785877dcf7fba92307131818c75",
				 "status":4,
				 "totalvotes":12745,
				 "optionsresult":[  
					{  
					   "option":{  
						  "id":"no",
						  "description":"Don't approve proposal",
						  "bits":1
					   },
					   "votesreceived":754
					},
					{  
					   "option":{  
						  "id":"yes",
						  "description":"Approve proposal",
						  "bits":2
					   },
					   "votesreceived":11991
					}
				 ],
				 "endheight":"289500",
				 "numofeligiblevotes":40958,
				 "quorumpercentage":20,
				 "passpercentage":60
			  }
		   ]
		}`
		}
		w.Write([]byte(resp))
	}))
}

// mockedPayload defines the complete unmarshalled sing payload returned by the
// mocked handleGetRequests.
var mockedPayload = &pitypes.ProposalInfo{
	ID:          2,
	Name:        "Change language: PoS Mining to PoS Voting, Stakepool to Voting Service Provider",
	State:       2,
	Status:      4,
	Timestamp:   1539880429,
	UserID:      "350a4b6c-5cdd-4d87-822a-4900dc3a930c",
	Username:    "richard-red",
	PublicKey:   "cd6e57b93f95dd0386d670c7ce42cb0ccd1cd5b997e87a716e9359e20251994e",
	Signature:   "c0e3285d447fd2acf1f2e1a0c86a71383dfe71b1b01e0068e56e8e7649dadb7aa503a5f99765fc3a24da8716fd5b89f75bb97762e756f15303e96d135a2e7109",
	NumComments: 19,
	// Files:         []pitypes.AttachmentFile{},
	Version:       "1",
	PublishedDate: 1539898457,
	Censorship: pitypes.CensorshipRecord{
		Token:      "522652954ea7998f3fca95b9c4ca8907820eb785877dcf7fba92307131818c75",
		MerkleRoot: "20c9234c50e0dc78d28003fd57995192a16ca73349f5d97be456128984e463fc",
		Signature:  "d1d44788cdf8d838aad97aa829b2f27f8a32897010d6373c9d3ca1a42820dcafe2615c1904558c6628c5f9165691ead087c0cb2ada023b9aa3f76b6c587ac90e",
	},
	ProposalVotes: pitypes.ProposalVotes{
		Token:      "522652954ea7998f3fca95b9c4ca8907820eb785877dcf7fba92307131818c75",
		VoteStatus: 4,
		TotalVotes: 12745,
		VoteResults: []pitypes.Results{
			{
				Option: pitypes.VoteOption{
					OptionID:    "no",
					Description: "Don't approve proposal",
					Bits:        1,
				},
				VotesReceived: 754,
			},
			{
				Option: pitypes.VoteOption{
					OptionID:    "yes",
					Description: "Approve proposal",
					Bits:        2,
				},
				VotesReceived: 11991,
			},
		},
		Endheight:          "289500",
		NumOfEligibleVotes: 40958,
		QuorumPercentage:   20,
		PassPercentage:     60,
	},
}

// TestStuff tests the update functionality, all proposals retrieval and proposal
// Retreival by ID.
func TestStuff(t *testing.T) {
	server := mockServer()
	newDBInstance := &ProposalDB{
		dbP:        db,
		client:     server.Client(),
		APIURLpath: server.URL,
	}

	defer server.Close()

	// Testing the update functionality
	t.Run("Test_CheckProposalsUpdates", func(t *testing.T) {
		err := newDBInstance.CheckProposalsUpdates()
		if err != nil {
			t.Fatalf("expected no error to be returned but found '%v'", err)
		}
	})

	// Testing the retrieval of all proposals
	t.Run("Test_AllProposals", func(t *testing.T) {
		offset := 0
		limit := 10
		proposals, count, err := newDBInstance.AllProposals(offset, limit)
		if err != nil {
			t.Fatal(err)
		}

		if len(proposals) != 2 {
			t.Fatalf("expected to find two proposals but found %d", len(proposals))
		}

		if count != 2 {
			t.Fatalf("expected to find count equal to 2 but found %d", count)
		}

		for _, data := range proposals {
			if data == nil {
				t.Fatal("expected the proposal not to be nil but was nil")
			}

			switch data.Censorship.Token {
			case firstProposal.Censorship.Token:
				if !reflect.DeepEqual(data, firstProposal) {
					t.Fatal("expected the initialProposal to match the retrieved but it did not")
				}

			case mockedPayload.Censorship.Token:
				if !reflect.DeepEqual(data, mockedPayload) {
					t.Fatal("expected the Second Proposal to match the retrieved but it did not")
				}

			default:
				t.Fatal("unknown incorrect data returned")
			}
		}
	})

	// Testing proposals Retrieval by vote status
	t.Run("Test_AllProposals_By_VoteStatus", func(t *testing.T) {
		offset := 0
		limit := 10
		voteStatus := 4 // Status "Finished".
		proposals, count, err := newDBInstance.AllProposals(offset, limit, voteStatus)
		if err != nil {
			t.Fatal(err)
		}

		if len(proposals) != 1 {
			t.Fatalf("expected to find one proposal but found %d", len(proposals))
		}

		if count != 1 {
			t.Fatalf("expected to find count equal to 1 but found %d", count)
		}

		if !reflect.DeepEqual(proposals[0], mockedPayload) {
			t.Fatal("expected the Second Proposal to match the retrieved but it did not")
		}
	})

	// Testing proposal retrieval by ID
	t.Run("Test_ProposalByID", func(t *testing.T) {
		proposal, err := newDBInstance.ProposalByID(1)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(proposal, firstProposal) {
			t.Fatal("expected the initialProposal to match the retrieved but it did not")
		}
	})
}
