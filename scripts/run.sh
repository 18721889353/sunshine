#!/bin/bash

serverName="serverNameExample_mixExample"
binaryFile="cmd/${serverName}/${serverName}"
configFile=$1
enableCC=$2
cmdArg=$3

# 简写处理：./scripts/run.sh true 相当于 ./scripts/run.sh "" true
if [ "$configFile" = "true" ] && [ -z "$enableCC" ]; then
    enableCC="true"
    configFile=""
fi

osType=$(uname -s)
if [ "${osType%%_*}"x = "MINGW64"x ];then
    binaryFile="${binaryFile}.exe"
fi

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

function stopService(){
    local NAME=$1

    ID=`ps -ef | grep "${binaryFile}" | grep -v "$0" | grep -v "grep" | awk '{print $2}'`
    if [ -n "$ID" ]; then
        for id in $ID
        do
           kill -9 $id
           echo "Stopped ${NAME} service successfully, process ID=${ID}"
        done
    fi
}

# 仅停止模式
if [ "$cmdArg"x = "stop"x ] || [ "$1"x = "stop"x ] ;then
    stopService ${serverName}
    echo "Service ${serverName} has stopped"
    exit 0
fi

# 重启模式：先停止旧进程
stopService ${serverName}

sleep 0.2

# 编译
go build -o ${binaryFile} cmd/${serverName}/main.go
checkResult $?

# 默认配置文件与启用配置中心的处理
if [ -z "$configFile" ] && [ "$enableCC" = "true" ]; then
  configFile="configs/${serverName}_cc.yml"
fi
if [ -z "$configFile" ]; then
  configFile="configs/${serverName}.yml"
fi

# 前台运行
if [ "$enableCC" = "true" ]; then
  ./${binaryFile} -enable-cc -c $configFile
else
  ./${binaryFile} -c $configFile
fi
