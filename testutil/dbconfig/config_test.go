package dbconfig

import (
	"bytes"
	"testing"
)

// TestScanQueries tests if scanQueries can be able to scan an individual query from
// many queries together as a single token.
func TestScanQueries(t *testing.T) {
	testData := []byte(`
		--
		-- Data for Name: test_addresses; Type: TABLE DATA; Schema: public; Owner: postgres
		--

		INSERT INTO public.test_addresses (id, address) VALUES (727, 'DseF2UooQ8R78U6ZkPxEkUfzvyWMS3uVZQk');
		INSERT INTO public.test_addresses (id, address) VALUES (62, 'DshdKtxhqX6Mi4iyvbYitaUYKE8M1FDyg9L');
		INSERT INTO public.test_addresses (id, address) VALUES (1976, 'DsosVxMiaBCq8qdaFAMDPFPoYkeXMAYSXME');
		`)

	isEOF := false
	val, token, err := scanQueries(testData, isEOF)
	if err != nil {
		t.Fatalf("expected no error to be returned but found %v", err)
	}

	if val <= 10 {
		t.Fatalf("expected the advance int value to greater than 10 but it was %v ", val)
	}

	// "test_" table name prefix references were deleted.
	// SQL comments were deleted.
	// Any leading or trailing spaces were deleted.
	// expectedToken shows the first token returned based on the testData input provided.
	expectedToken := []byte(`INSERT INTO public.addresses (id, address) VALUES (727, 'DseF2UooQ8R78U6ZkPxEkUfzvyWMS3uVZQk')`)

	if !bytes.Equal(token, expectedToken) {
		t.Fatalf("expected the returned token to be (%v) but found (%v)", string(expectedToken), string(token))
	}
}
