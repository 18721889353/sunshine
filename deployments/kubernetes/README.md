# k8s 部署说明

## 前置条件

部署前需在已登录镜像仓库的主机上创建镜像拉取 Secret（凭证不入仓，`scripts/deploy-k8s.sh` 部署前会校验其存在性）：

```bash
# 1. 先登录镜像仓库(阿里云镜像服务使用固定密码), 登录后会生成 /root/.docker/config.json
#docker login --username=<账号> <镜像仓库地址>

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: project-name-example
  labels:
    app: server-name-example
EOF

# 2. 基于登录凭证创建 Secret, 注意 -n 必须与 Deployment 所在命名空间一致
kubectl create secret generic docker-auth-secret \
    -n project-name-example \
    --from-file=.dockerconfigjson=/root/.docker/config.json \
    --type=kubernetes.io/dockerconfigjson
    
    
#手动创建 tencent-cloud-credentials Secret（一次性）    
kubectl create secret generic tencent-cloud-credentials \
  -n project-name-exampleo \
  --from-literal=secret_id='你的SecretId' \
  --from-literal=secret_key='你的SecretKey'    
  
#首次证书同步：手动触发（或等待自动执行）
kubectl create job --from=cronjob/cert-sync test-sync -n project-name-example

#如果 CronJob 未成功创建 domain-tls（手动应急方案）
kubectl create secret tls domain-tls \
  -n project-name-example \
  --cert=/home/docker/ssl/domain.pem \
  --key=/home/docker/ssl/domain.key
  
      
```

## 部署

```bash
./scripts/deploy-k8s.sh
```

查看启动状态：

> kubectl get all -n project-name-example

<br>

http 端口简单测试

```bash
# 将服务的 http 端口转发到本地端口
kubectl port-forward --address=0.0.0.0 service/server-name-example-svc 8080:8001 -n project-name-example
```
