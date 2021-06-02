package politeia

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/decred/politeia/politeiad/plugins/usermd"
	umplugin "github.com/decred/politeia/politeiad/plugins/usermd"
	piv1 "github.com/decred/politeia/politeiawww/api/pi/v1"
	recordsv1 "github.com/decred/politeia/politeiawww/api/records/v1"
)

// userMetadataDecode returns the parsed data for the usermd plugin metadata
// stream.
func userMetadataDecode(ms []recordsv1.MetadataStream) (*usermd.UserMetadata, error) {
	var userMD *usermd.UserMetadata
	for _, m := range ms {
		if m.PluginID != usermd.PluginID ||
			m.StreamID != usermd.StreamIDUserMetadata {
			// This is not user metadata
			continue
		}
		var um usermd.UserMetadata
		err := json.Unmarshal([]byte(m.Payload), &um)
		if err != nil {
			return nil, err
		}
		userMD = &um
		break
	}
	return userMD, nil
}

// proposalMetadataDecode returns the parsed data for the pi plugin proposal
// metadata stream.
func proposalMetadataDecode(fs []recordsv1.File) (*piv1.ProposalMetadata, error) {
	var pmp *piv1.ProposalMetadata
	for _, f := range fs {
		if f.Name != piv1.FileNameProposalMetadata {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(f.Payload)
		if err != nil {
			return nil, err
		}
		var pm piv1.ProposalMetadata
		err = json.Unmarshal(b, &pm)
		if err != nil {
			return nil, err
		}
		pmp = &pm
		break
	}
	if pmp == nil {
		return nil, fmt.Errorf("proposal metadata not found")
	}
	return pmp, nil
}

// statusChangeMetadataDecode returns the published, censored and abandoned
// dates from status change metadata streams.
func statusChangeMetadataDecode(md []recordsv1.MetadataStream) ([]uint64, string, error) {
	var (
		statuses = make([]umplugin.StatusChangeMetadata, 0, 16)
	)
	for _, v := range md {
		if v.PluginID != umplugin.PluginID {
			continue
		}

		// Search for status change metadata
		switch v.StreamID {
		case umplugin.StreamIDStatusChanges:
			d := json.NewDecoder(strings.NewReader(v.Payload))
			for {
				var sc umplugin.StatusChangeMetadata
				err := d.Decode(&sc)
				if errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					return nil, "", err
				}
				statuses = append(statuses, sc)
			}
		}
	}

	// Parse timestamps according to status
	var (
		publishedAt, censoredAt, abandonedAt int64
		changeMsg                            string
		changeMsgTimestamp                   int64
	)
	for _, v := range statuses {
		if v.Timestamp > changeMsgTimestamp {
			changeMsg = v.Reason
			changeMsgTimestamp = v.Timestamp
		}
		switch recordsv1.RecordStatusT(v.Status) {
		case recordsv1.RecordStatusPublic:
			publishedAt = v.Timestamp
		case recordsv1.RecordStatusCensored:
			censoredAt = v.Timestamp
		case recordsv1.RecordStatusArchived:
			abandonedAt = v.Timestamp
		}
	}

	return []uint64{
		uint64(publishedAt),
		uint64(censoredAt),
		uint64(abandonedAt),
	}, changeMsg, nil
}
