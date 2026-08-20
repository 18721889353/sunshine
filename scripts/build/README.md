
- `Dockerfile`: 直接复制编译好的二进制文件构建镜像，构建速度快。
- `Dockerfile_build`: 两阶段构建镜像，构建速度较慢，可指定 golang 版本。
- `Dockerfile_test`: 用于测试 rpc 服务的容器。
