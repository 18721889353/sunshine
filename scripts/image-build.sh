#!/bin/bash

# 使用编译好的二进制文件构建 docker 镜像；如需减小镜像体积，
# 可在构建镜像前先用 upx 压缩二进制文件。

serverName="serverNameExample_mixExample"
# 服务镜像名称，名称中禁止使用大写字母。
IMAGE_NAME="project-name-example/server-name-example"
# Dockerfile 文件所在目录
DOCKERFILE_PATH="scripts/build"
DOCKERFILE="${DOCKERFILE_PATH}/Dockerfile"

# 镜像仓库地址，REPO_HOST="ip 或域名"，通过第一个参数传入
REPO_HOST=$1
if [ "X${REPO_HOST}" = "X" ];then
        echo "param 'repo host' cannot be empty, example: ./image-build.sh hub.docker.com v1.0.0"  # 参数 'repo host' 不能为空，示例：./image-build.sh hub.docker.com v1.0.0
        exit 1
fi
# 版本标签，为空时默认为 latest，通过第二个参数传入
TAG=$2
if [ "X${TAG}" = "X" ];then
        TAG="latest"
fi
# 镜像名称及标签
IMAGE_NAME_TAG="${REPO_HOST}/${IMAGE_NAME}:${TAG}"

# 二进制可执行文件
BIN_FILE="cmd/${serverName}/${serverName}"
# 配置文件目录
CONFIG_PATH="configs"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ${BIN_FILE} cmd/${serverName}/*.go
mv -f ${BIN_FILE} ${DOCKERFILE_PATH}
mkdir -p ${DOCKERFILE_PATH}/${CONFIG_PATH} && cp -f ${CONFIG_PATH}/${serverName}.yml ${CONFIG_PATH}/${serverName}_cc.yml ${DOCKERFILE_PATH}/${CONFIG_PATH}

# todo generate image-build code for http or grpc here
# delete the templates code start

# k8s 探针已改用原生 grpc: 方式（见 deployment.yml），无需再编译安装 grpc_health_probe

# 压缩二进制文件
#cd ${DOCKERFILE_PATH}
#upx -9 ${serverName}
#cd -

echo "docker build -f ${DOCKERFILE} -t ${IMAGE_NAME_TAG} ${DOCKERFILE_PATH}"
docker build -f ${DOCKERFILE} -t ${IMAGE_NAME_TAG} ${DOCKERFILE_PATH}

# delete the templates code end

if [ -f "${DOCKERFILE_PATH}/${serverName}" ]; then
    rm -f ${DOCKERFILE_PATH}/${serverName}
fi

if [ -d "${DOCKERFILE_PATH}/configs" ]; then
    rm -rf ${DOCKERFILE_PATH}/configs
fi

# 删除 <none> 标签的悬空镜像
noneImages=$(docker images | grep "<none>" | awk '{print $3}')
if [ "X${noneImages}" != "X" ]; then
  docker rmi ${noneImages} > /dev/null
fi
exit 0
