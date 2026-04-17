#!/bin/bash

patchType=$1
typesPb="types-pb"
initMysql="mysql"
initPostgresql="postgresql"

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

function importPkg() {
    go mod tidy
}

function generateTypesPbCode() {
    sunshine patch gen-types-pb --out=./
    checkResult $?
}

function generateInitMysqlCode() {
    sunshine patch gen-db-init --db-driver=mysql --out=./
    checkResult $?
    importPkg
}

function generateInitPostgresqlCode() {
    sunshine patch gen-db-init --db-driver=postgresql --out=./
    checkResult $?
    importPkg
}

if [  "$patchType" = "$typesPb"  ]; then
    generateTypesPbCode
elif [ "$patchType" = "$initMysql" ] || [ "$patchType" == "init-$initMysql" ]; then
    generateInitMysqlCode
elif [ "$patchType" = "$initPostgresql" ] || [ "$patchType" == "init-$initPostgresql" ]; then
    generateInitPostgresqlCode
else
    echo "invalid patch type: '$patchType'"
    echo "supported types: $initMysql, $initPostgresql, $typesPb"
    echo "e.g. make patch TYPE=$initMysql"
    echo ""
    exit 1
fi
