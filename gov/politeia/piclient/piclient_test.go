package piclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

// TestHandleGetRequests Tests the HandleGetRequests functionality.
func TestHandleGetRequests(t *testing.T) {
	type testsData struct {
		// Inputs
		client      *http.Client
		_APIURLPath string

		// Outputs
		data   []byte
		errMsg string
	}

	// This is a mock server that should handle the requests locally.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "OK")
	}))

	testClient := server.Client()
	testURL := server.URL

	td := []testsData{
		{nil, "", []byte(""), "invalid http client was passed"},
		{testClient, "", []byte(""), "empty API URL is not supported"},
		{nil, testURL, []byte(""), "invalid http client was passed"},
		{testClient, testURL, []byte("OK"), ""},
	}

	// tests if the expected result is returned.
	for i, item := range td {
		t.Run("Test_"+strconv.Itoa(i), func(t *testing.T) {
			results, err := HandleGetRequests(item.client, item._APIURLPath)
			if err != nil {
				if err.Error() != item.errMsg {
					t.Fatalf("expected the error message to be '%s' but found '%v'", item.errMsg, err)
				}
			} else {
				if item.errMsg != "" {
					t.Fatalf("expected no error message but found '%s'", item.errMsg)
				}
			}

			if reflect.DeepEqual(results, item.data) {
				t.Fatalf("expected the result returned to be '%+v' but found '%+v'", item.data, results)
			}
		})
	}
}
