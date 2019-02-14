// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package agendadb

import (
	"fmt"
	"os"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/rpcclient/v2"
)

// AgendaDB represents the data for the saved db
type AgendaDB struct {
	sdb        *storm.DB
	NumAgendas int
	NumChoices int
}

var dbPath string

// Open will either open and existing database or create a new one using with
// the specified file name.
func Open() (*AgendaDB, error) {
	_, err := os.Stat(dbPath)
	isNewDB := err != nil && !os.IsNotExist(err)

	db, err := storm.Open(dbPath)
	if err != nil {
		return nil, err
	}

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

	agendaDB := &AgendaDB{
		sdb:        db,
		NumAgendas: numAgendas,
		NumChoices: numChoices,
	}
	return agendaDB, err
}

// SetDbPath sets the dbPath as fetched from the configuration
func SetDbPath(path string) {
	dbPath = path
}

// Close should be called when you are done with the AgendaDB to close the
// underlying database.
func (db *AgendaDB) Close() error {
	return db.sdb.Close()
}

// StoreAgenda saves an agenda in the database.
func (db *AgendaDB) StoreAgenda(agenda *AgendaTagged) error {
	if db == nil || db.sdb == nil {
		return fmt.Errorf("AgendaDB not initialized")
	}
	return db.sdb.Save(agenda)
}

// LoadAgenda retrieves an agenda corresponding to the specified unique agenda
// ID, or nil if it does not exist.
func (db *AgendaDB) LoadAgenda(agendaID string) (*AgendaTagged, error) {
	if db == nil || db.sdb == nil {
		return nil, fmt.Errorf("AgendaDB not initialized")
	}
	agenda := new(AgendaTagged)
	if err := db.sdb.One("Id", agendaID, agenda); err != nil {
		return nil, err
	}
	return agenda, nil
}

// ListAgendas lists all agendas stored in the database in order of StartTime.
func (db *AgendaDB) ListAgendas() error {
	if db == nil || db.sdb == nil {
		return fmt.Errorf("AgendaDB not initialized")
	}
	q := db.sdb.Select().OrderBy("StartTime")
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
func (db *AgendaDB) updatedb(voteVersion uint32, client *rpcclient.Client) {
	var agendas []AgendaTagged
	for agendasForVoteVersion(voteVersion, client) != nil {
		taggedAgendas := agendasForVoteVersion(voteVersion, client)
		if len(taggedAgendas) > 0 {
			agendas = append(agendas, taggedAgendas...)
			voteVersion++
		}
	}
	for i := range agendas {
		err := db.StoreAgenda(&agendas[i])
		if err != nil {
			log.Errorf("Agenda not saved: %v \n", err)
		}
	}
}

// CheckForUpdates checks for update at the start of the process and will
// proceed to update when necessary.
func CheckForUpdates(client *rpcclient.Client) error {
	adb, err := Open()
	if err != nil {
		log.Errorf("Failed to open new DB: %v", err)
		return nil
	}
	// voteVersion is vote version as of when lnsupport and sdiffalgorithm votes
	// casting was activated. More information can be found here
	// https://docs.decred.org/getting-started/user-guides/agenda-voting/#voting-archive
	// Also at the moment all agenda version information available in the rpc
	// starts from version 4 by default.

	var voteVersion uint32 = 4
	for adb.IsAgendasAvailable(voteVersion) {
		voteVersion++
	}
	adb.updatedb(voteVersion, client)

	return adb.Close()
}

// AgendaInfo fetches an agenda's details given it's agendaID.
func AgendaInfo(agendaID string) (*AgendaTagged, error) {
	adb, err := Open()
	if err != nil {
		log.Errorf("Failed to open new DB: %v", err)
		return nil, err
	}

	agenda, err := adb.LoadAgenda(agendaID)
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
	if err != nil {
		log.Errorf("Failed to open new Agendas DB: %v", err)
		return
	}

	defer func() {
		err = adb.Close()
		if err != nil {
			log.Errorf("Failed to close the Agendas DB: %v", err)
		}
	}()

	err = adb.sdb.All(&agendas)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}

	return
}
