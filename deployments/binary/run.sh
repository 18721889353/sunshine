#!/bin/bash

serviceName="serverNameExample"
cmdStr="./${serviceName}"
configFile=$1
enableCC=$2
cmdArg=$3

# 简写处理：./run.sh true 相当于 ./run.sh "" true
if [ "$configFile" = "true" ] && [ -z "$enableCC" ]; then
    enableCC="true"
    configFile=""
fi

chmod +x ./${serviceName}

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

stopService(){
    local NAME=$1

    ID=`ps -ef | grep "$NAME" | grep -v "$0" | grep -v "grep" | awk '{print $2}'`
    if [ -n "$ID" ]; then
        for id in $ID
        do
           kill -9 $id
           echo "Stopped ${NAME} service successfully, process ID=${ID}"
        done
    fi
}

startService() {
    local NAME=$1

    # 默认配置文件和启用配置中心处理
    if [ -z "$configFile" ] && [ "$enableCC" = "true" ]; then
      configFile="configs/${NAME}_cc.yml"
    fi
    if [ -z "$configFile" ]; then
      configFile="configs/${NAME}.yml"
    fi

    if [ "$enableCC" = "true" ]; then
        nohup ${cmdStr} -enable-cc -c $configFile > ${NAME}.log 2>&1 &
    else
        nohup ${cmdStr} -c $configFile > ${NAME}.log 2>&1 &
    fi
    sleep 1

    ID=`ps -ef | grep "$NAME" | grep -v "$0" | grep -v "grep" | awk '{print $2}'`
    if [ -n "$ID" ]; then
        echo "Start the ${NAME} service ...... process ID=${ID}"
    else
        echo "Failed to start ${NAME} service"
        return 1
    fi
    return 0
}


stopService ${serviceName}
if [ "$cmdArg"x = "stop"x ] || [ "$1"x = "stop"x ] ;then
    echo "Service ${serviceName} has stopped"
else
    sleep 1
    startService ${serviceName}
    checkResult $?
fi
