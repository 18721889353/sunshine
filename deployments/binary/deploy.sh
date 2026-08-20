#!/bin/bash

serviceName="serverNameExample"

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

# 判断启动服务脚本 run.sh 是否存在
runFile="~/app/${serviceName}/run.sh"
if [ ! -f "$runFile" ]; then
  # 不存在则复制整个目录
  mkdir -p ~/app
  cp -rf /tmp/${serviceName}-binary ~/app/
  checkResult $?
  rm -rf /tmp/${serviceName}-binary*
else
  # 已存在则仅替换二进制文件
  cp -f ${serviceName}-binary/${serviceName} ~/app/${serviceName}-binary/${serviceName}
  checkResult $?
  rm -rf /tmp/${serviceName}-binary*
fi

# 运行服务，将所有参数透传给 run.sh
cd ~/app/${serviceName}-binary
chmod +x run.sh
./run.sh "$1" "$2" "$3"
checkResult $?

echo "server directory is ~/app/${serviceName}-binary"
