// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package api

import "fmt"

type version struct {
	Major, Minor, Patch int
	Label               string
	Nick                string
}

var ver = version{
	Major: 1,
	Minor: 0,
	Patch: 0,
	Label: ""}

// CommitHash may be set on the build command line:
// go build -ldflags "-X main.CommitHash=`git rev-parse --short HEAD`"
var CommitHash string

const appName string = "dcrdata-api"

func (v *version) String() string {
	var hashStr string
	if CommitHash != "" {
		hashStr = "+" + CommitHash
	}
	if v.Label != "" {
		return fmt.Sprintf("%d.%d.%d-%s%s",
			v.Major, v.Minor, v.Patch, v.Label, hashStr)
	}
	return fmt.Sprintf("%d.%d.%d%s",
		v.Major, v.Minor, v.Patch, hashStr)
}
