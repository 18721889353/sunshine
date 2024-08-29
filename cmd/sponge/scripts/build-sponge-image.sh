#!/bin/bash

TAG=$1
if [ "X${TAG}" = "X" ];then
    echo "image tag cannot be empty, example: ./build-sunshine-image.sh v1.5.8"
    exit 1
fi

function rmFile() {
    sFile=$1
    if [ -e "${sFile}" ]; then
        rm -rf ${sFile}
    fi
}

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

# download the specified version of the sunshine binary file
binaryFile="sunshine_${TAG#v}_linux_amd64.zip"
rmFile ${binaryFile}
wget https://github.com/18721889353/sunshine/releases/download/${TAG}/${binaryFile}
checkResult $?
unzip -o -q ${binaryFile}
rmFile ${binaryFile} && rmFile LICENSE && rmFile README.md

# download the specified version of the sunshine template code
codeFile="${TAG}.zip"
rmFile ${codeFile}
wget https://github.com/18721889353/sunshine/archive/refs/tags/${codeFile}
checkResult $?
unzip -o -q ${codeFile}
mv sunshine-${TAG#v} .sunshine
echo ${TAG} > .sunshine/.github/version
rmFile ${codeFile} && rm -rf .sunshine/cmd/sunshine

# compressing binary file
upx -9 sunshine
checkResult $?

echo "docker build -t 18721889353/sunshine:${TAG}  ."
docker build -t 18721889353/sunshine:${TAG}  .
checkResult $?

rmFile sunshine
rm -rf .sunshine

# delete none image
noneImages=$(docker images | grep "<none>" | awk '{print $3}')
if [ "X${noneImages}" != "X" ]; then
  docker rmi ${noneImages} > /dev/null
fi
