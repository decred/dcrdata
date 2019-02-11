// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package agendadb

import (
	"fmt"
	"net/http"
	"os"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/rpcclient/v2"
)

// AgendaDB represents the data for the saved db
type AgendaDB struct {
	sdb        *storm.DB
	onNode     storm.Node // On chain Node
	offNode    storm.Node // off chain Node
	NumAgendas int
	NumChoices int

	client      *http.Client
	politeiaURL string
}

var dbPath, politeiaAPIURL string

// Open will either open and existing database or create a new one using with
// the specified file name.
func Open() (*AgendaDB, error) {
	_, err := os.Stat(dbPath)
	isNewDB := err != nil && !os.IsNotExist(err)

	db, err := storm.Open(dbPath)
	if err != nil {
		return nil, err
	}

	offChainNode := db.From("offchain")
	onChainNode := db.From("onchain")

	var numAgendas, numChoices int
	if !isNewDB {
		numAgendas, err = db.Count(&AgendaTagged{})
		if err != nil {
			log.Errorf("Agendas count failed: %v\n", err)
		}
		numChoices, err = db.Count(&ChoiceLabeled{})
		if err != nil {
			log.Errorf("Choices count failed: %v\n", err)
		}
	}

	if politeiaAPIURL == "" {
		return nil, fmt.Errorf("missing politeia API URL")
	}

	agendaDB := &AgendaDB{
		sdb:         db,
		onNode:      onChainNode,
		offNode:     offChainNode,
		NumAgendas:  numAgendas,
		NumChoices:  numChoices,
		client:      fetchHTTPClient(),
		politeiaURL: politeiaAPIURL,
	}
	return agendaDB, err
}

// SetConfig sets the dbPath and the politeia root API URL endpoint as fetched
// from the configuration
func SetConfig(path, url string) {
	dbPath = path
	politeiaAPIURL = url
}

// Close should be called when you are done with the AgendaDB to close the
// underlying database.
func (db *AgendaDB) Close() error {
	return db.sdb.Close()
}

// storeAgenda saves an agenda in the database.
func (db *AgendaDB) storeAgenda(agenda *AgendaTagged) error {
	return db.onNode.Save(agenda)
}

// oadAgenda retrieves an agenda corresponding to the specified unique agenda
// ID, or nil if it does not exist.
func (db *AgendaDB) loadAgenda(agendaID string) (*AgendaTagged, error) {
	agenda := new(AgendaTagged)
	if err := db.onNode.One("Id", agendaID, agenda); err != nil {
		return nil, err
	}

	return agenda, nil
}

// ListAgendas lists all agendas stored in the database in order of StartTime.
func (db *AgendaDB) ListAgendas() error {
	if db == nil || db.onNode == nil {
		return fmt.Errorf("AgendaDB not initialized")
	}
	q := db.onNode.Select().OrderBy("StartTime")
	i := 0
	return q.Each(new(AgendaTagged), func(record interface{}) error {
		a, ok := record.(*AgendaTagged)
		if !ok {
			return fmt.Errorf("record is not of type *AgendaTagged")
		}
		log.Infof("%d: %s\n", i, a.Id)
		i++
		return nil
	})
}

// AgendaTagged has the same fields as dcrjson.Agenda, but with the Id field
// marked as the primary key via the `storm:"id"` tag. Fields tagged for
// indexing by the DB are: StartTime, ExpireTime, Status, and QuorumProgress.
type AgendaTagged struct {
	Id             string           `json:"id" storm:"id"`
	Description    string           `json:"description"`
	Mask           uint16           `json:"mask"`
	StartTime      uint64           `json:"starttime" storm:"index"`
	ExpireTime     uint64           `json:"expiretime" storm:"index"`
	Status         string           `json:"status" storm:"index"`
	QuorumProgress float64          `json:"quorumprogress" storm:"index"`
	Choices        []dcrjson.Choice `json:"choices"`
	VoteVersion    uint32           `json:"voteversion"`
}

// ChoiceLabeled embeds dcrjson.Choice along with the AgendaID for the choice,
// and a string array suitable for use as a primary key. The AgendaID is tagged
// as an index for quick lookups based on the agenda.
type ChoiceLabeled struct {
	AgendaChoice   [2]string `storm:"id"`
	AgendaID       string    `json:"agendaid" storm:"index"`
	dcrjson.Choice `storm:"inline"`
}

// agendasForVoteVersion fetches the agendas using the vote versions provided.
func agendasForVoteVersion(ver uint32, client *rpcclient.Client) (agendas []AgendaTagged) {
	voteInfo, err := client.GetVoteInfo(ver)
	if err != nil {
		log.Errorf("Fetching Agendas by vote version failed: %v", err)
	} else {
		for i := range voteInfo.Agendas {
			v := &voteInfo.Agendas[i]
			a := AgendaTagged{
				Id:             v.ID,
				Description:    v.Description,
				Mask:           v.Mask,
				StartTime:      v.StartTime,
				ExpireTime:     v.ExpireTime,
				Status:         v.Status,
				QuorumProgress: v.QuorumProgress,
				Choices:        v.Choices,
				VoteVersion:    voteInfo.VoteVersion,
			}
			agendas = append(agendas, a)
		}
	}
	return
}

// IsAgendasAvailable checks for the availabily of agendas in the db by vote version.
func (db *AgendaDB) IsAgendasAvailable(version uint32) bool {
	if db == nil || db.sdb == nil {
		return false
	}

	agenda := make([]AgendaTagged, 0)
	err := db.sdb.Find("VoteVersion", version, &agenda)
	if len(agenda) == 0 || err != nil {
		return false
	}

	return true
}

// updatedb used when needed to keep the saved db upto date.
func (db *AgendaDB) updatedb(voteVersion uint32, client *rpcclient.Client) int {
	var agendas []AgendaTagged
	for agendasForVoteVersion(voteVersion, client) != nil {
		taggedAgendas := agendasForVoteVersion(voteVersion, client)
		if len(taggedAgendas) > 0 {
			agendas = append(agendas, taggedAgendas...)
			voteVersion++
		}
	}
	for i := range agendas {
		err := db.storeAgenda(&agendas[i])
		if err != nil {
			log.Errorf("Agenda not saved: %v \n", err)
		}
	}

	return len(agendas)
}

// CheckForUpdates checks for update at the start of the process and will
// proceed to update when necessary.
func CheckForUpdates(client *rpcclient.Client) error {
	adb, err := Open()
	if err != nil || adb == nil || adb.onNode == nil || adb.offNode == nil {
		return fmt.Errorf("Failed to open new DB: %v", err)
	}

	numRecords := adb.checkOnChainUpdates(client)
	log.Infof("%d on-chain records (agendas) were updated", numRecords)

	numRecords, err = adb.checkOffChainUpdates()
	if err != nil {
		return fmt.Errorf("checkOffChainUpdates failed : %v", err)
	}

	log.Infof("%d off-chain records (politea proposals) were updated", numRecords)

	return adb.Close()
}

func (db *AgendaDB) checkOnChainUpdates(client *rpcclient.Client) int {
	// voteVersion is vote version as of when lnsupport and sdiffalgorithm votes
	// casting was activated. More information can be found here
	// https://docs.decred.org/getting-started/user-guides/agenda-voting/#voting-archive
	// Also at the moment all agenda version information available in the rpc
	// starts from version 4 by default.
	var voteVersion uint32 = 4
	for db.IsAgendasAvailable(voteVersion) {
		voteVersion++
	}

	return db.updatedb(voteVersion, client)
}

// AgendaInfo fetches an agenda's details given it's agendaID.
func AgendaInfo(agendaID string) (*AgendaTagged, error) {
	adb, err := Open()
	if err != nil || adb == nil || adb.onNode == nil {
		log.Errorf("Failed to open new DB: %v", err)
		return nil, err
	}

	agenda, err := adb.loadAgenda(agendaID)
	if err != nil {
		_ = adb.Close() // only return the LoadAgenda error
		return nil, err
	}

	return agenda, adb.Close()
}

// AllAgendas returns all agendas and their info in the db.
func AllAgendas() (agendas []*AgendaTagged, err error) {
	var adb *AgendaDB
	adb, err = Open()
	if err != nil || adb == nil || adb.onNode == nil {
		log.Errorf("Failed to open new Agendas DB: %v", err)
		return
	}

	defer func() {
		err = adb.Close()
		if err != nil {
			log.Errorf("Failed to close the Agendas DB: %v", err)
		}
	}()

	err = adb.onNode.All(&agendas)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}

	return
}
