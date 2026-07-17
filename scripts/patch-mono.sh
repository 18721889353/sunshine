#!/bin/bash

goModFile="go.mod"
thirdPartyProtoDir="third_party"
genServerType=$1

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

if [ ! -f "../$goModFile" ]; then
    sunshine patch copy-go-mod -f
    checkResult $?
    mv -f go.mod ..
    mv -f go.sum ..
fi

if [ "$genServerType"x != "http"x ]; then
    if [ ! -d "../$thirdPartyProtoDir" ]; then
        sunshine patch copy-third-party-proto
        checkResult $?
        mv -f $thirdPartyProtoDir ..
    fi
fi

function getModuleName() {
  if [ -f "../go.mod" ]; then
    head -1 ../go.mod | awk '{print $2}'
  fi
}

if [ "$genServerType"x = "grpc"x ]; then
    if [ ! -d "../api/types" ]; then
        moduleName=$(getModuleName)
        if [ -z "$moduleName" ]; then
            echo "Error: go.mod not found, cannot determine module name"
            exit 1
        fi
        sunshine patch gen-types-pb --module-name="$moduleName" --out=.
        checkResult $?
        mv -f api/types ../api
        rmdir api
    fi
fi
