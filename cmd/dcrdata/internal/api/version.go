// Copyright (c) 2017-2021, The dcrdata developers
// See LICENSE for details.

package api

// APIVersion is an integer value, incremented for breaking changes.
type APIVersion int32

// currentVersion is the most recent version supported.
const currentAPIVersion = 1

// supportedAPIVersions is a list of supported API versions.
var supportedAPIVersions = [...]APIVersion{
	currentAPIVersion,
}

// CommitHash may be set on the build command line:
// go build -ldflags "-X main.CommitHash=`git rev-parse --short HEAD`"
var CommitHash string
