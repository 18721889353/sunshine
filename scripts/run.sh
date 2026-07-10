#!/bin/bash

serverName="fuliApiGo"

binaryFile="cmd/${serverName}/${serverName}"
configFile=$1
enableCC=$2

osType=$(uname -s)
if [ "${osType%%_*}"x = "MINGW64"x ];then
    binaryFile="${binaryFile}.exe"
fi

if [ -f "${binaryFile}" ] ;then
     rm "${binaryFile}"
fi

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

sleep 0.2

go build -o ${binaryFile} cmd/${serverName}/main.go
checkResult $?

# running server
if [ -z "$configFile" ] && [ "$enableCC" = "true" ]; then
  configFile="configs/${serverName}_cc.yml"
fi

if [ -n "$configFile" ]; then
  if [ "$enableCC" = "true" ]; then
    ./${binaryFile} -enable-cc -c $configFile
  else
    ./${binaryFile} -c $configFile
  fi
else
  ./${binaryFile}
fi
