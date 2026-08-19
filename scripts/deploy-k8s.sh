#!/bin/bash

SERVER_NAME="serverNameExample"
DEPLOY_PATH="deployments/kubernetes"
NAMESPACE_FILE="${DEPLOY_PATH}/projectNameExample-namespace.yml"
CONFIGMAP_FILE="${DEPLOY_PATH}/${SERVER_NAME}-configmap.yml"
SVC_FILE="${DEPLOY_PATH}/${SERVER_NAME}-svc.yml"
DEPLOY_FILE="${DEPLOY_PATH}/${SERVER_NAME}-deployment.yml"

NAMESPACE_NAME="project-name-example"
DEPLOY_NAME="server-name-example-dm"
SECRET_NAME="docker-auth-secret"

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

# 判断部署文件是否全部存在
for file in "${NAMESPACE_FILE}" "${CONFIGMAP_FILE}" "${SVC_FILE}" "${DEPLOY_FILE}"; do
  if [ ! -f "${file}" ]; then
    echo "File ${file} does not exist"
    checkResult 1
  fi
done

# 检查是否有权限操作 k8s 集群
echo "kubectl version"
kubectl version
checkResult $?

# 删除旧 Deployment(--ignore-not-found 兼容首次部署), configmap/svc/namespace 由 apply 幂等更新
echo "kubectl delete -f ${DEPLOY_FILE} --ignore-not-found"
kubectl delete -f ${DEPLOY_FILE} --ignore-not-found
checkResult $?

sleep 1

# 按依赖顺序应用资源: namespace -> configmap -> svc -> deployment
echo "kubectl apply -f ${NAMESPACE_FILE}"
kubectl apply -f ${NAMESPACE_FILE}
checkResult $?

# 镜像拉取密钥: Deployment 的 imagePullSecrets 引用
# 出于安全考虑密钥不入仓, 需在已登录镜像仓库的主机上提前手动创建
if ! kubectl get secret "${SECRET_NAME}" -n "${NAMESPACE_NAME}" >/dev/null 2>&1; then
  echo "Secret ${SECRET_NAME} does not exist, please create it first:"
  echo "kubectl create secret generic ${SECRET_NAME} --from-file=.dockerconfigjson=/root/.docker/config.json --type=kubernetes.io/dockerconfigjson -n ${NAMESPACE_NAME}"
  checkResult 1
fi

# 配置中心连接配置(ConfigMap), Deployment 启动时挂载该文件
echo "kubectl apply -f ${CONFIGMAP_FILE}"
kubectl apply -f ${CONFIGMAP_FILE}
checkResult $?

echo "kubectl apply -f ${SVC_FILE}"
kubectl apply -f ${SVC_FILE}
checkResult $?

echo "kubectl apply -f ${DEPLOY_FILE}"
kubectl apply -f ${DEPLOY_FILE}
checkResult $?

# 等待 Deployment 滚动发布完成, 失败时及时暴露问题
echo "kubectl rollout status deployment/${DEPLOY_NAME} -n ${NAMESPACE_NAME}"
kubectl rollout status deployment/${DEPLOY_NAME} -n ${NAMESPACE_NAME} --timeout=120s
checkResult $?

# 输出部署结果, 便于确认 Pod/Service 状态
echo "kubectl get all -n ${NAMESPACE_NAME}"
kubectl get all -n ${NAMESPACE_NAME}
