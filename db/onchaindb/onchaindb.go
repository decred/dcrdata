// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package onchaindb

import (
	"fmt"
	"os"

	"github.com/asdine/storm"
	dcrjson "github.com/decred/dcrd/dcrjson/v2"
	rpcclient "github.com/decred/dcrd/rpcclient/v2"
)

// AgendaDB represents the data for the saved db
type AgendaDB struct {
	sdb        *storm.DB
	NumAgendas int
	NumChoices int
}

// AgendaTagged has the same fields as dcrjson.Agenda, but with the Id field
// marked as the primary key via the `storm:"id"` tag. Fields tagged for
// indexing by the DB are: StartTime, ExpireTime, Status, and QuorumProgress.
type AgendaTagged struct {
	ID             string           `json:"id" storm:"id"`
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

// errDefault defines an error message returned if the agenda db wasn't
// properly initialized.
var errDefault = fmt.Errorf("AgendaDB was not initialized correctly")

// NewOnChainDB opens an existing database or create a new one using with
// the specified file name. An initialized on-chain db connection is returned.
func NewOnChainDB(dbPath string) (*AgendaDB, error) {
	_, err := os.Stat(dbPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	db, err := storm.Open(dbPath)
	if err != nil {
		return nil, err
	}

	return &AgendaDB{sdb: db}, nil
}

// countProperties fetches the count of Agendas and Choices. It the appends them
// to the AgendaDB receiver.
func (db *AgendaDB) countProperties() error {
	numAgendas, err := db.sdb.Count(&AgendaTagged{})
	if err != nil {
		log.Errorf("Agendas count failed: %v\n", err)
		return err
	}

	numChoices, err := db.sdb.Count(&ChoiceLabeled{})
	if err != nil {
		log.Errorf("Choices count failed: %v\n", err)
		return err
	}

	db.NumAgendas = numAgendas
	db.NumChoices = numChoices
	return nil
}

// Close should be called when you are done with the AgendaDB to close the
// underlying database.
func (db *AgendaDB) Close() error {
	if db == nil || db.sdb == nil {
		return nil
	}
	return db.sdb.Close()
}

// oadAgenda retrieves an agenda corresponding to the specified unique agenda
// ID, or nil if it does not exist.
func (db *AgendaDB) loadAgenda(agendaID string) (*AgendaTagged, error) {
	agenda := new(AgendaTagged)
	if err := db.sdb.One("ID", agendaID, agenda); err != nil {
		return nil, err
	}

	return agenda, nil
}

// agendasForVoteVersion fetches the agendas using the vote versions provided.
func agendasForVoteVersion(ver uint32, client *rpcclient.Client) (agendas []AgendaTagged) {
	voteInfo, err := client.GetVoteInfo(ver)
	if err != nil {
		log.Errorf("Fetching Agendas by vote version failed: %v", err)
	} else {
		for i := range voteInfo.Agendas {
			v := &voteInfo.Agendas[i]
			agendas = append(agendas, AgendaTagged{
				ID:             v.ID,
				Description:    v.Description,
				Mask:           v.Mask,
				StartTime:      v.StartTime,
				ExpireTime:     v.ExpireTime,
				Status:         v.Status,
				QuorumProgress: v.QuorumProgress,
				Choices:        v.Choices,
				VoteVersion:    voteInfo.VoteVersion,
			})
		}
	}
	return
}

// IsAgendasAvailable checks for the availabily of agendas in the db by vote version.
func (db *AgendaDB) isAgendasAvailable(version uint32) bool {
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

// storeAgenda saves an agenda in the database.
func (db *AgendaDB) storeAgenda(agenda *AgendaTagged) error {
	return db.sdb.Save(agenda)
}

// CheckForUpdates checks for update at the start of the process and will
// proceed to update when necessary.
func (db *AgendaDB) CheckForUpdates(client *rpcclient.Client) error {
	if db == nil || db.sdb == nil {
		return errDefault
	}

	// voteVersion is vote version as of when lnsupport and sdiffalgorithm votes
	// casting was activated. More information can be found here
	// https://docs.decred.org/getting-started/user-guides/agenda-voting/#voting-archive
	// Also at the moment all agenda version information available in the rpc
	// starts from version 4 by default.
	var voteVersion uint32 = 4
	for db.isAgendasAvailable(voteVersion) {
		voteVersion++
	}

	numRecords := db.updatedb(voteVersion, client)
	log.Infof("%d on-chain records (agendas) were updated", numRecords)

	return db.countProperties()
}

// AgendaInfo fetches an agenda's details given it's agendaID.
func (db *AgendaDB) AgendaInfo(agendaID string) (*AgendaTagged, error) {
	if db == nil || db.sdb == nil {
		return nil, errDefault
	}

	agenda, err := db.loadAgenda(agendaID)
	if err != nil {
		return nil, err
	}

	return agenda, nil
}

// AllAgendas returns all agendas and their info in the db.
func (db *AgendaDB) AllAgendas() (agendas []*AgendaTagged, err error) {
	if db == nil || db.sdb == nil {
		return nil, errDefault
	}

	err = db.sdb.All(&agendas)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}
	return
}
