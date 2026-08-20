#!/bin/bash

# chkconfig: - 85 15
# description: serverNameExample

serverName="serverNameExample_mixExample"
cmdStr="cmd/${serverName}/${serverName}"
pidFile="cmd/${serverName}/${serverName}.pid"
configFile=$1
enableCC=$2
cmdArg=$3


function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

# 简写处理：./scripts/run-nohup.sh true 相当于 ./scripts/run-nohup.sh "" true
if [ "$configFile" = "true" ] && [ -z "$enableCC" ]; then
    enableCC="true"
    configFile=""
fi

function stopService(){
    local NAME=$1

    # 优先通过 pid 文件终止进程
    if [ -f "${pidFile}" ]; then
        local pid=$(cat "${pidFile}")
        local processInfo=`ps -p "${pid}" | grep "${cmdStr}"`
        if [ -n "${processInfo}" ]; then
           kill -9 ${pid}
           checkResult $?
           echo "Stopped ${NAME} service successfully, process ID=${pid}"
           rm -f ${pidFile}
           return 0
        fi
    fi

    # pid 文件不存在时，通过进程名查找并终止进程
    ID=`ps -ef | grep "${cmdStr}" | grep -v "$0" | grep -v "grep" | awk '{print $2}'`
    if [ -n "$ID" ]; then
        for id in $ID
        do
           kill -9 $id
           echo "Stopped ${NAME} service successfully, process ID=${ID}"
           return 0
        done
    fi
}

function startService() {
    local NAME=$1

    sleep 0.2
    go build -o ${cmdStr} cmd/${NAME}/main.go
    checkResult $?

    # 默认配置文件和启用配置中心处理
    if [ -z "$configFile" ] && [ "$enableCC" = "true" ]; then
      configFile="configs/${NAME}_cc.yml"
    fi
    if [ -z "$configFile" ]; then
      configFile="configs/${NAME}.yml"
    fi

    # 后台运行服务，日志追加写入文件
    if [ "$enableCC" = "true" ]; then
        nohup ${cmdStr} -enable-cc -c $configFile >> ${NAME}.log 2>&1 &
    else
        nohup ${cmdStr} -c $configFile >> ${NAME}.log 2>&1 &
    fi

    local pid=$!
    printf "%s" "${pid}" > "${pidFile}"
    sleep 1

    # 检查进程是否在运行，使用更可靠的方法
    if ps -p "${pid}" > /dev/null 2>&1; then
        echo "Started the ${NAME} service successfully, process ID=${pid}"
    else
        # 如果直接检查PID失败，尝试通过进程名检查
        if ps aux | grep "${cmdStr}" | grep -v grep > /dev/null 2>&1; then
            echo "Started the ${NAME} service successfully, process ID=${pid}"
        else
            echo "Failed to start ${NAME} service"
            rm -f ${pidFile}
		    return 1
        fi
    fi
    return 0
}

stopService ${serverName}
if [ "$cmdArg"x = "stop"x ] || [ "$1"x = "stop"x ] ;then
    echo "Service ${serverName} has stopped"
else
    sleep 1
    startService ${serverName}
    checkResult $?
fi
