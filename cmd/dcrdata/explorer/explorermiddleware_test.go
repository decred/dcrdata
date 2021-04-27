// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package explorer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMenuFormParser(t *testing.T) {
	// Dummy hander for use with the MenuFormParser middleware.
	blah := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "blah")
	})
	// The handler wrapping blah with MenuFormParser.
	handler := MenuFormParser(blah)

	// TEST with no form data. Expected response body: "blah"
	r := httptest.NewRequest("POST", "/set", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	//resp := w.Result()

	if w.Body.String() != "blah" {
		t.Errorf("MenuFormParser failed to call dummy handler's ServeHTTP.")
	}

	// TEST with form with requestURIFormKey ("requestURI"). Expect 302 redirect
	// to the relative URL (path, no scheme, no host, no query, no fragment).
	form := url.Values{}
	form.Add(darkModeFormKey, "1")
	form.Add(requestURIFormKey, "https://explorer.decred.org/blocks?junk=1#fraggle")

	r = httptest.NewRequest("POST", "/set", strings.NewReader(form.Encode()))
	r.Header.Add("Content-Type", "application/x-www-form-urlencoded") // Required!
	// darkCookie := &http.Cookie{
	// 	Name:   darkModeCoookie,
	// 	Value:  "1",
	// 	MaxAge: 0,
	// }
	// r.AddCookie(darkCookie)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	resp := w.Result()

	if w.Body.String() != "" {
		t.Errorf("MenuFormParser failed to respond with an empty body. Got: %v.",
			w.Body.String())
	}

	if resp.StatusCode != http.StatusFound {
		t.Errorf("MenuFormParser failed to respond with status 302 FOUND. Got: %v.",
			resp.StatusCode)
	}

	loc, err := resp.Location()
	if err != nil {
		t.Errorf(`Location header not found or invalid: "%v"`, err)
	}
	if loc.String() != "/blocks" {
		t.Errorf(`Location header not set to "/blocks", got "%s".`, loc)
	}
	if loc.IsAbs() {
		t.Errorf(`Location should have been relative, was absolute: "%s"`, loc)
	}
	if len(loc.Query()) > 0 {
		t.Errorf(`Location included a query, should have been an escaped path. Loc: "%s"`, loc)
	}
	if loc.String() != loc.EscapedPath() {
		t.Errorf(`Location should have been JUST an escaped path, got "%s"`, loc)
	}
	if loc.Host != "" {
		t.Errorf("Location should not have a host, but it was %s", loc.Host)
	}
}
