#!/bin/bash

# 两阶段构建 docker 镜像

serverName="serverNameExample_mixExample"
# 服务镜像名称，名称中禁止使用大写字母。
IMAGE_NAME="project-name-example/server-name-example"
# Dockerfile 文件所在目录
DOCKERFILE_PATH="scripts/build"
DOCKERFILE="${DOCKERFILE_PATH}/Dockerfile_build"

# 镜像仓库地址，REPO_HOST="ip 或域名"，通过第一个参数传入
REPO_HOST=$1
if [ "X${REPO_HOST}" = "X" ];then
        echo "param 'repo host' cannot be empty, example: ./image-build.sh hub.docker.com v1.0.0"
        exit 1
fi
# 版本标签，为空时默认为 latest，通过第二个参数传入
TAG=$2
if [ "X${TAG}" = "X" ];then
        TAG="latest"
fi
# 镜像名称及标签
IMAGE_NAME_TAG="${REPO_HOST}/${IMAGE_NAME}:${TAG}"

# 构建上下文为项目根目录，Dockerfile_build 通过 go.mod/go.sum 利用 Docker 分层缓存
echo "docker build --force-rm -f ${DOCKERFILE} -t ${IMAGE_NAME_TAG} ."
docker build --force-rm -f ${DOCKERFILE} -t ${IMAGE_NAME_TAG} .

# 删除 <none> 标签的悬空镜像
noneImages=$(docker images | grep "<none>" | awk '{print $3}')
if [ "X${noneImages}" != "X" ]; then
  docker rmi ${noneImages} > /dev/null
fi
exit 0

