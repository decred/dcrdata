package politeia

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/decred/politeia/politeiad/plugins/usermd"
	piv1 "github.com/decred/politeia/politeiawww/api/pi/v1"
	recordsv1 "github.com/decred/politeia/politeiawww/api/records/v1"
	ticketvotev1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
)

// userMetadataDecode returns the parsed data for the usermd plugin metadata
// stream.
func userMetadataDecode(ms []recordsv1.MetadataStream) (*usermd.UserMetadata, error) {
	for _, m := range ms {
		if m.PluginID != usermd.PluginID || m.StreamID != usermd.StreamIDUserMetadata {
			// This is not user metadata.
			continue
		}
		var um usermd.UserMetadata
		err := json.Unmarshal([]byte(m.Payload), &um)
		if err != nil {
			return nil, err
		}
		return &um, nil
	}
	return nil, fmt.Errorf("user metadata not found")
}

// proposalMetadataDecode returns the parsed data for the pi plugin proposal
// metadata stream.
func proposalMetadataDecode(fs []recordsv1.File) (*piv1.ProposalMetadata, error) {
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
		return &pm, nil
	}
	return nil, fmt.Errorf("proposal metadata not found")
}

// statusTimestamps contains the published, censored and abandoned timestamps
// that are used on dcrdata's UI.
type statusTimestamps struct {
	published int64
	censored  int64
	abandoned int64
}

// statusChangeMetadataDecode returns the published, censored and abandoned
// dates from status change metadata streams in the statusTimestamps struct.
// It also returns the status change message for the latest metadata stream.
func statusChangeMetadataDecode(md []recordsv1.MetadataStream) (*statusTimestamps, string, error) {
	var statuses []usermd.StatusChangeMetadata
	for _, v := range md {
		if v.PluginID != usermd.PluginID || v.StreamID != usermd.StreamIDStatusChanges {
			// This is not status change metadata.
			continue
		}

		// The metadata payload is a stream of encoded json objects.
		// Decode the payload accordingly.
		d := json.NewDecoder(strings.NewReader(v.Payload))
		for {
			var sc usermd.StatusChangeMetadata
			err := d.Decode(&sc)
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, "", err
			}
			statuses = append(statuses, sc)
		}
	}

	// Status change metadata represents the data associated with each status
	// change a proposal undergoes. A proposal can only ever be on one status
	// once. Therefore, walk the statuses metadatas and parse the data we need
	// from the public, censored and abandoned status, as well as the status
	// change message from the latest one.
	var (
		timestamps         statusTimestamps
		changeMsg          string
		changeMsgTimestamp int64
	)
	for _, v := range statuses {
		if v.Timestamp > changeMsgTimestamp {
			changeMsg = v.Reason
			changeMsgTimestamp = v.Timestamp
		}
		switch recordsv1.RecordStatusT(v.Status) {
		case recordsv1.RecordStatusPublic:
			timestamps.published = v.Timestamp
		case recordsv1.RecordStatusCensored:
			timestamps.censored = v.Timestamp
		case recordsv1.RecordStatusArchived:
			timestamps.abandoned = v.Timestamp
		}
	}

	return &timestamps, changeMsg, nil
}

// voteBitVerify verifies that the vote bit corresponds to a valid vote option.
// This verification matches the one used on politeia's code base to verify
// vote bits.
func voteBitVerify(options []ticketvotev1.VoteOption, mask, bit uint64) error {
	if len(options) == 0 {
		return fmt.Errorf("no vote options found")
	}
	if bit == 0 {
		return fmt.Errorf("invalid bit %#x", bit)
	}

	// Verify bit is included in mask
	if mask&bit != bit {
		return fmt.Errorf("invalid mask 0x%x bit %#x", mask, bit)
	}

	// Verify bit is included in vote options
	for _, v := range options {
		if v.Bit == bit {
			// Bit matches one of the options. We're done.
			return nil
		}
	}

	return fmt.Errorf("bit %#x not found in vote options", bit)
}
