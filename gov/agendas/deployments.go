// Copyright (c) 2018-2019, The Decred developers
// See LICENSE for details.

// Package agendas manages the various deployment agendas that are directly
// voted upon with the vote bits in vote transactions.
package agendas

import (
	"fmt"
	"os"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson/v2"
)

// AgendaDB represents the data for the stored DB.
type AgendaDB struct {
	sdb        *storm.DB
	NumAgendas int
}

// AgendaTagged has the same fields as dcrjson.Agenda plus the VoteVersion
// field, but with the ID field marked as the primary key via the `storm:"id"`
// tag. Fields tagged for indexing by the DB are: StartTime, ExpireTime, Status,
// and QuorumProgress.
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

// errDefault defines an error message returned if the agenda db wasn't properly
// initialized.
var errDefault = fmt.Errorf("AgendaDB was not initialized correctly")

// DeploymentSource provides a cleaner way to track the rpcclient methods used
// in this package. It also allows usage of alternative implementations to
// satisfy the interface.
type DeploymentSource interface {
	GetVoteInfo(version uint32) (*dcrjson.GetVoteInfoResult, error)
}

// NewAgendasDB opens an existing database or create a new one using with the
// specified file name. An initialized agendas db connection is returned.
func NewAgendasDB(dbPath string) (*AgendaDB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("empty db Path found")
	}

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

// countProperties fetches the Agendas count and appends it to the AgendaDB
// receiver.
func (db *AgendaDB) countProperties() error {
	numAgendas, err := db.sdb.Count(&AgendaTagged{})
	if err != nil {
		log.Errorf("Agendas count failed: %v\n", err)
		return err
	}

	db.NumAgendas = numAgendas
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

// loadAgenda retrieves an agenda corresponding to the specified unique agenda
// ID, or returns nil if it does not exist.
func (db *AgendaDB) loadAgenda(agendaID string) (*AgendaTagged, error) {
	agenda := new(AgendaTagged)
	if err := db.sdb.One("ID", agendaID, agenda); err != nil {
		return nil, err
	}

	return agenda, nil
}

// agendasForVoteVersion fetches the agendas using the vote versions provided.
func agendasForVoteVersion(ver uint32, client DeploymentSource) ([]AgendaTagged, error) {
	voteInfo, err := client.GetVoteInfo(ver)
	if err != nil {
		return nil, err
	}

	// Set the agendas slice capacity.
	agendas := make([]AgendaTagged, 0, len(voteInfo.Agendas))
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

	return agendas, nil
}

// updatedb checks if vote versions available in chaincfg.ConsensusDeployment
// are already updated in the agendas db, if not yet their data is updated.
// dcrjson.GetVoteInfoResult and chaincfg.ConsensusDeployment hold almost similar
// data contents but chaincfg.Vote does not contain the important vote status
// field that is found in dcrjson.Agenda.
func (db *AgendaDB) updatedb(client DeploymentSource,
	activeVersions map[uint32][]chaincfg.ConsensusDeployment) (int, error) {
	var agendas []AgendaTagged
	for voteVersion := range activeVersions {
		taggedAgendas, err := agendasForVoteVersion(voteVersion, client)
		if err != nil || len(taggedAgendas) == 0 {
			return -1, fmt.Errorf("vote version %d agendas retrieval failed: %v",
				voteVersion, err)
		}

		agendas = append(agendas, taggedAgendas...)
	}

	for i := range agendas {
		agenda := &agendas[i]
		err := db.storeAgenda(agenda)
		if err != nil {
			return -1, fmt.Errorf("agenda '%s' was not saved: %v",
				agenda.Description, err)
		}
	}

	return len(agendas), nil
}

// storeAgenda saves an agenda in the database.
func (db *AgendaDB) storeAgenda(agenda *AgendaTagged) error {
	return db.sdb.Save(agenda)
}

// CheckAgendasUpdates checks for update at the start of the process and will
// proceed to update when necessary.
func (db *AgendaDB) CheckAgendasUpdates(client DeploymentSource,
	activeVersions map[uint32][]chaincfg.ConsensusDeployment) error {
	if db == nil || db.sdb == nil {
		return errDefault
	}

	if len(activeVersions) == 0 {
		return nil
	}

	numRecords, err := db.updatedb(client, activeVersions)
	if err != nil {
		return fmt.Errorf("agendas.CheckAgendasUpdates failed: %v", err)
	}

	log.Infof("%d agenda records (agendas) were updated", numRecords)

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
