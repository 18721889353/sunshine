FROM alpine:latest

LABEL maintainer="18721889353 <g.18721889353@gmail.com>"

# 设置上海时区
ENV TZ=Asia/Shanghai
RUN apk add --no-cache tzdata

# 创建非 root 用户，提升安全性
RUN adduser -D -u 1001 appuser

# todo generate dockerfile code for http or grpc here
# delete the templates code start

# 安装 curl，用于 http 服务健康检查；若部署在 k8s 可不安装
RUN apk add curl

# k8s 探针已改用原生 grpc: 方式（见 deployment.yml），无需在镜像内安装 grpc_health_probe

COPY --chown=appuser:appuser configs/ /app/configs/
COPY --chown=appuser:appuser serverNameExample /app/serverNameExample
RUN chmod +x /app/serverNameExample
# delete the templates code end

# 切换到非 root 用户
USER appuser

WORKDIR /app

# 服务端口 6001 性能分析和指标端口 grpc 端口 7001 http 端口 8001
EXPOSE 7001 8001 6001

#CMD ["./serverNameExample", "-c", "configs/serverNameExample.yml"]
# 如使用配置中心，将 serverNameExample.yml 改为配置中心的配置文件。
CMD ["./serverNameExample", "-c", "configs/serverNameExample.yml", "-enable-cc"]
