#!/bin/bash
# Usage: build.sh [dcrdata_root] [destination_folder]
#
#   build.sh performs the following actions:
#       1. Compile go code, generating the main binary.
#       2. Install webpack dependencies via npm install.
#       3. Build the frontend files via npm run build, which creates the
#          public/dist folder.
#       4. Gzip the compressible static assets.
#       5. (Optional) Install everything.
#
#   When run with no arguments, build.sh will use the repository root as the
#   root folder. If not running from a git repository, the dcrdata_root folder
#   must be specified.
#
#   Specify destination_folder to install the dcrdata executable and the static
#   assets (public and views folders). When destination_folder is omitted, the
#   generated files are not installed.
#
#   Note that this script uses 7za to Gzip static assets. The standard gzip
#   utility is not used since 7za compression rates are slightly better even for
#   the gz format.
#
# Copyright (c) 2018-2020, The Decred developers.
# See LICENSE for details.

REPO=`git rev-parse --show-toplevel 2> /dev/null`
if [[ $? != 0 ]]; then
    REPO=
fi

ROOT=${1:-$REPO}

if [[ -z "$ROOT" ]]; then
    echo "Not in git repository. You must specify the dcrdata root folder as the first argument!"
    exit 1
fi

set -e

# Delete the old dcrdata binary that is now under cmd/dcrdata.
rm -f ${ROOT}/dcrdata

pushd $ROOT/cmd/dcrdata > /dev/null

echo "Building the dcrdata binary..."
GO111MODULE=on go build -v

echo "Packaging static frontend assets..."
npm clean-install
npm run build

echo "Gzipping assets for use with gzip_static..."
find ./public -type f -name "*.gz" -execdir rm {} \;
# Use GNU parallel if it is installed.
FINDCMD='find ./public -type f -not -name "*.gz" -not -name "*.scss" -not -name "*.png" -not -name "*.woff2"'
if [ -x "$(command -v parallel)" ]; then
    if [ -x "$(command -v 7za)" ]; then
        $FINDCMD | parallel --will-cite --bar 7za a -tgzip -mx=9 -mpass=13 {}.gz {} > /dev/null
    else
        $FINDCMD | parallel --will-cite --bar gzip -k9f {} > /dev/null
    fi
elif [ -x "$(command -v 7za)" ]; then
    $FINDCMD -execdir 7za a -tgzip -mx=9 -mpass=13 {}.gz {} \; > /dev/null    
else
    $FINDCMD -execdir gzip -k9f {} \; > /dev/null
fi

# Clean up incompressible and files not part of the distribution.
find ./public -type f -name "*.png.gz" -execdir rm {} \;
find ./public -type f -name "*.scss.gz" -execdir rm {} \;
find ./public -type f -name "*.woff2.gz" -execdir rm {} \;
find ./public -type f -name "*.gz.gz" -execdir rm {} \;

DEST=$2

if [[ -n "$DEST" ]]; then
    sudo install -m 754 -o dcrdata -g decred ./dcrdata ${DEST}/
    sudo rm -rf ${DEST}/views ${DEST}/public
    sudo cp -R views public ${DEST}/
fi

popd > /dev/null

echo "Done"
