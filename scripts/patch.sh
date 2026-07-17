#!/bin/bash

patchType=$1
typesPb="types-pb"
initMysql="mysql"

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

function importPkg() {
    go mod tidy
}

function getModuleName() {
  if [ -f "go.mod" ]; then
    head -1 go.mod | awk '{print $2}'
  fi
}

function generateTypesPbCode() {
    moduleName=$(getModuleName)
    if [ -z "$moduleName" ]; then
        echo "Error: go.mod not found, cannot determine module name"
        exit 1
    fi
    sunshine patch gen-types-pb --module-name="$moduleName" --out=./
    checkResult $?
}

function generateInitMysqlCode() {
    moduleName=$(getModuleName)
    if [ -z "$moduleName" ]; then
        echo "Error: go.mod not found, cannot determine module name"
        exit 1
    fi
    sunshine patch gen-db-init --db-driver=mysql --module-name="$moduleName" --out=./
    checkResult $?
    importPkg
}

if [  "$patchType" = "$typesPb"  ]; then
    generateTypesPbCode
elif [ "$patchType" = "$initMysql" ] || [ "$patchType" == "init-$initMysql" ]; then
    generateInitMysqlCode
else
    echo "invalid patch type: '$patchType'"
    echo "supported types: $initMysql, $typesPb"
    echo "e.g. make patch TYPE=$initMysql"
    echo ""
    exit 1
fi
