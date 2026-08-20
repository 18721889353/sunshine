#!/bin/bash

# 构建 rpc 服务测试镜像

serverName="serverNameExample_mixExample"
# 服务镜像名称，名称中禁止使用大写字母。
IMAGE_NAME="project-name-example/server-name-example.rpc-test"
# Dockerfile 文件所在目录
DOCKERFILE_PATH="scripts/build"
DOCKERFILE="${DOCKERFILE_PATH}/Dockerfile_test"

# 镜像仓库地址，REPO_HOST="ip 或域名"，通过第一个参数传入
REPO_HOST=$1
if [ "X${REPO_HOST}" = "X" ];then
        echo "param 'repo host' cannot be empty, example: ./image-rpc-test.sh hub.docker.com v1.0.0"
        exit 1
fi
# 版本标签，为空时默认为 latest，通过第二个参数传入
TAG=$2
if [ "X${TAG}" = "X" ];then
        TAG="latest"
fi
# 镜像名称及标签
IMAGE_NAME_TAG="${REPO_HOST}/${IMAGE_NAME}:${TAG}"

PROJECT_FILES=$(ls)
tar zcf ${serverName}.tar.gz ${PROJECT_FILES}
mv -f ${serverName}.tar.gz ${DOCKERFILE_PATH}

echo "docker build -f ${DOCKERFILE} -t ${IMAGE_NAME_TAG} ${DOCKERFILE_PATH}"
docker build --force-rm -f ${DOCKERFILE} -t ${IMAGE_NAME_TAG} ${DOCKERFILE_PATH}

rm -rf ${DOCKERFILE_PATH}/${serverName}.tar.gz
