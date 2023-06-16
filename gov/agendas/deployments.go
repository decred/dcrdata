// Copyright (c) 2018-2021, The Decred developers
// See LICENSE for details.

// Package agendas manages the various deployment agendas that are directly
// voted upon with the vote bits in vote transactions.
package agendas

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
	"github.com/decred/dcrd/dcrjson/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/semver"
)

// AgendaDB represents the data for the stored DB.
type AgendaDB struct {
	sdb           *storm.DB
	stakeVersions []uint32
	deploySource  DeploymentSource
}

// AgendaTagged has the same fields as chainjson.Agenda plus the VoteVersion
// field, but with the ID field marked as the primary key via the `storm:"id"`
// tag. Fields tagged for indexing by the DB are: StartTime, ExpireTime, Status,
// and QuorumProgress.
type AgendaTagged struct {
	ID             string                   `json:"id" storm:"id"`
	Description    string                   `json:"description"`
	Mask           uint16                   `json:"mask"`
	StartTime      uint64                   `json:"starttime" storm:"index"`
	ExpireTime     uint64                   `json:"expiretime" storm:"index"`
	Status         dbtypes.AgendaStatusType `json:"status" storm:"index"`
	QuorumProgress float64                  `json:"quorumprogress" storm:"index"`
	Choices        []chainjson.Choice       `json:"choices"`
	VoteVersion    uint32                   `json:"voteversion"`
}

var (
	// dbVersion is the current required version of the agendas.db.
	dbVersion = semver.NewSemver(1, 0, 0)
)

// dbInfo defines the property that holds the db version.
const dbInfo = "_agendas.db_"

// DeploymentSource provides a cleaner way to track the rpcclient methods used
// in this package. It also allows usage of alternative implementations to
// satisfy the interface.
type DeploymentSource interface {
	GetVoteInfo(ctx context.Context, version uint32) (*chainjson.GetVoteInfoResult, error)
}

// NewAgendasDB opens an existing database or create a new one using with the
// specified file name. It also checks the DB version, reindexes the DB if need
// be, and sets the required DB version.
func NewAgendasDB(client DeploymentSource, dbPath string) (*AgendaDB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("empty db Path found")
	}

	if client == DeploymentSource(nil) {
		return nil, fmt.Errorf("invalid deployment source found")
	}

	_, err := os.Stat(dbPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	db, err := storm.Open(dbPath)
	if err != nil {
		return nil, err
	}

	// Check if the correct DB version has been set.
	var version string
	err = db.Get(dbInfo, "version", &version)
	if err != nil && err != storm.ErrNotFound {
		return nil, err
	}

	// Check if the versions match.
	if version != dbVersion.String() {
		// Attempt to delete AgendaTagged bucket.
		if err = db.Drop(&AgendaTagged{}); err != nil {
			// If error due bucket not found was returned ignore it.
			if !strings.Contains(err.Error(), "not found") {
				return nil, fmt.Errorf("delete bucket struct failed: %v", err)
			}
		}

		// Set the required db version.
		err = db.Set(dbInfo, "version", dbVersion.String())
		if err != nil {
			return nil, err
		}
		log.Infof("agendas.db version %v was set", dbVersion)
	}

	// Determine stake versions known by dcrd.
	stakeVersions, err := listStakeVersions(client)
	if err != nil {
		return nil, err
	}

	adb := &AgendaDB{
		sdb:           db,
		deploySource:  client,
		stakeVersions: stakeVersions,
	}
	return adb, nil
}

func listStakeVersions(client DeploymentSource) ([]uint32, error) {
	agendaIDs := func(agendas []chainjson.Agenda) (ids []string) {
		for i := range agendas {
			ids = append(ids, agendas[i].ID)
		}
		return
	}

	var firstVer uint32
	for {
		voteInfo, err := client.GetVoteInfo(context.TODO(), firstVer)
		if err == nil {
			// That's the first version.
			log.Debugf("Stake version %d: %v", firstVer, agendaIDs(voteInfo.Agendas))
			// startTime = voteInfo.Agendas[0].StartTime
			break
		}

		if jerr, ok := err.(*dcrjson.RPCError); ok &&
			jerr.Code == dcrjson.ErrRPCInvalidParameter {
			firstVer++
			if firstVer == 10 {
				log.Warnf("No stake versions found < 10. aborting scan")
				return nil, nil
			}
			continue
		}

		return nil, err
	}

	versions := []uint32{firstVer}
	for i := firstVer + 1; ; i++ {
		voteInfo, err := client.GetVoteInfo(context.TODO(), i)
		if err == nil {
			log.Debugf("Stake version %d: %v", i, agendaIDs(voteInfo.Agendas))
			versions = append(versions, i)
			continue
		}

		if jerr, ok := err.(*dcrjson.RPCError); ok &&
			jerr.Code == dcrjson.ErrRPCInvalidParameter {
			break
		}

		// Something went wrong.
		return nil, err
	}

	return versions, nil
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
	voteInfo, err := client.GetVoteInfo(context.TODO(), ver)
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
			Status:         dbtypes.AgendaStatusFromStr(v.Status),
			QuorumProgress: v.QuorumProgress,
			Choices:        v.Choices,
			VoteVersion:    voteInfo.VoteVersion,
		})
	}

	return agendas, nil
}

// updateDB updates the agenda data for all configured vote versions.
// chainjson.GetVoteInfoResult and chaincfg.ConsensusDeployment hold almost
// similar data contents but chaincfg.Vote does not contain the important vote
// status field that is found in chainjson.Agenda.
func (db *AgendaDB) updateDB() (int, error) {
	agendas := make([]AgendaTagged, 0, len(db.stakeVersions))
	for _, voteVersion := range db.stakeVersions {
		taggedAgendas, err := agendasForVoteVersion(voteVersion, db.deploySource)
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

// UpdateAgendas updates agenda data for all configured vote versions.
func (db *AgendaDB) UpdateAgendas() error {
	if db.stakeVersions == nil {
		log.Debugf("skipping agendas update")
		return nil
	}

	numRecords, err := db.updateDB()
	if err != nil {
		return fmt.Errorf("agendas.UpdateAgendas failed: %v", err)
	}

	log.Infof("%d agenda records (agendas) were updated", numRecords)
	return nil
}

// AgendaInfo fetches an agenda's details given its agendaID.
func (db *AgendaDB) AgendaInfo(agendaID string) (*AgendaTagged, error) {
	if db.stakeVersions == nil {
		return nil, fmt.Errorf("No deployments")
	}

	agenda, err := db.loadAgenda(agendaID)
	if err != nil {
		return nil, err
	}

	return agenda, nil
}

// AllAgendas returns all agendas and their info in the db.
func (db *AgendaDB) AllAgendas() (agendas []*AgendaTagged, err error) {
	if db.stakeVersions == nil {
		return []*AgendaTagged{}, nil
	}

	err = db.sdb.Select(q.True()).OrderBy("VoteVersion", "ID").Reverse().Find(&agendas)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}
	return
}
