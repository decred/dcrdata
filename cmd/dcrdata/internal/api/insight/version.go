// Copyright (c) 2017-2022, The dcrdata developers
// See LICENSE for details.

package insight

// APIVersion is an integer value, incremented for breaking changes.
type APIVersion int32

// currentVersion is the most recent version supported.
const currentAPIVersion = 1

// supportedAPIVersions is a list of supported API versions.
var supportedAPIVersions = [...]APIVersion{
	currentAPIVersion,
}
