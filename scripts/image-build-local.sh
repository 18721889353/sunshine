#!/bin/bash

# 使用编译好的二进制文件为本地 docker 构建镜像；如需减小镜像体积，
# 可在构建镜像前先用 upx 压缩二进制文件。

serverName="serverNameExample_mixExample"
# 服务镜像名称，名称中禁止使用大写字母。
IMAGE_NAME="project-name-example/server-name-example"
# Dockerfile 文件所在目录
DOCKERFILE_PATH="scripts/build"
DOCKERFILE="${DOCKERFILE_PATH}/Dockerfile"

mv -f cmd/${serverName}/${serverName} ${DOCKERFILE_PATH}/${serverName}

# todo generate image-build-local code for http or grpc here
# delete the templates code start

# k8s 探针已改用原生 grpc: 方式（见 deployment.yml），无需再编译安装 grpc_health_probe

# 压缩二进制文件
#cd ${DOCKERFILE_PATH}
#upx -9 ${serverName}
#cd -

mkdir -p ${DOCKERFILE_PATH}/configs && cp -f configs/${serverName}.yml configs/${serverName}_cc.yml ${DOCKERFILE_PATH}/configs/
echo "docker build -f ${DOCKERFILE} -t ${IMAGE_NAME}:latest ${DOCKERFILE_PATH}"
docker build -f ${DOCKERFILE} -t ${IMAGE_NAME}:latest ${DOCKERFILE_PATH}

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
