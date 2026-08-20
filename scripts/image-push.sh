#!/bin/bash

# 镜像名称，名称中禁止使用大写字母。
IMAGE_NAME="project-name-example/server-name-example"

# 镜像仓库地址，通过第一个参数传入
REPO_HOST=$1
if [ "X${REPO_HOST}" = "X" ];then
    echo "param 'repo host' cannot be empty, example: ./image-push.sh hub.docker.com v1.0.0"
    exit 1
fi

# 版本标签，通过第二个参数传入，为空时默认为 latest
TAG=$2
if [ "X${TAG}" = "X" ];then
    TAG="latest"
fi
# 镜像名称及标签
IMAGE_NAME_TAG="${REPO_HOST}/${IMAGE_NAME}:${TAG}"

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

# 镜像仓库主机地址，https://index.docker.io/v1 为 docker 官方镜像仓库
IMAGE_REPO_HOST="image-repo-host"
# 检查是否已登录镜像仓库
function checkLogin() {
  loginStatus=$(cat /root/.docker/config.json | grep "${IMAGE_REPO_HOST}")
  if [ "X${loginStatus}" = "X" ];then
      echo "docker is not logged into the image repository"
      checkResult 1
  fi
}

checkLogin

# 推送镜像到镜像仓库
echo "docker push ${IMAGE_NAME_TAG}"
docker push ${IMAGE_NAME_TAG}
checkResult $?
echo "docker push image success."

sleep 1

# 删除本地镜像
echo "docker rmi -f ${IMAGE_NAME_TAG}"
docker rmi -f ${IMAGE_NAME_TAG}
checkResult $?
echo "docker remove image success."
