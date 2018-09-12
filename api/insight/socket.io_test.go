package insight

import (
	"encoding/json"
	"testing"
)

func TestMarshalInsightTx(t *testing.T) {
	isv := InsightSocketVout{
		Address: "DsZQaCQES5vh3JmcyyFokJYz3aSw8Sm1dsQ",
		Value:   13741789,
	}

	b, err := json.Marshal(isv)
	if err != nil {
		t.Errorf("Failed to marshal InsightSocketVout: %v", err)
	}

	t.Logf("%s", string(b))
	expectedJSON := `{"Address":"DsZQaCQES5vh3JmcyyFokJYz3aSw8Sm1dsQ","Value":13741789}`
	if string(b) != expectedJSON {
		t.Errorf("json.Marshal of InsightSocketVout incorrect. "+
			"Expected %s, got %s", expectedJSON, string(b))
	}
}

func TestMarshalInsightTxSlice(t *testing.T) {
	isv := []InsightSocketVout{
		{
			Address: "DsZQaCQES5vh3JmcyyFokJYz3aSw8Sm1dsQ",
			Value:   13741789,
		},
		{
			Address: "DsidNx5RnC7jMHdvUSEV54WANwMyKbyGtjh",
			Value:   1000000,
		},
	}

	b, err := json.Marshal(isv)
	if err != nil {
		t.Errorf("Failed to marshal InsightSocketVout: %v", err)
	}

	t.Logf("%s", string(b))
	expectedJSON := `[{"DsZQaCQES5vh3JmcyyFokJYz3aSw8Sm1dsQ":13741789},` +
		`{"DsidNx5RnC7jMHdvUSEV54WANwMyKbyGtjh":1000000}]`
	if string(b) != expectedJSON {
		t.Errorf("json.Marshal of InsightSocketVout incorrect. "+
			"Expected %s, got %s", expectedJSON, string(b))
	}
}
