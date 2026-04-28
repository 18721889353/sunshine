# ==============================================================================
# Sunshine 微服务框架 - Makefile
# 提供代码生成、构建、测试、部署等自动化任务
# ==============================================================================

SHELL := /bin/bash

# 项目名称和包路径
PROJECT_NAME := "github.com/18721889353/sunshine"
PKG := "$(PROJECT_NAME)"
# 获取所有 Go 包列表，排除 vendor 和 api 目录
PKG_LIST := $(shell go list ${PKG}/... | grep -v /vendor/ | grep -v /api/)

# delete the templates code start
.PHONY: install
# 安装依赖的 Protobuf 插件和开发工具
install:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest  # Protobuf Go 生成器
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest  # gRPC Go 生成器
	go install github.com/envoyproxy/protoc-gen-validate@latest      # Protobuf 验证插件
	go install github.com/srikrsna/protoc-gen-gotag@latest           # Protobuf tag 生成器
	go install github.com/18721889353/sunshine/cmd/protoc-gen-go-gin@latest  # Gin 路由生成器
	go install github.com/18721889353/sunshine/cmd/protoc-gen-go-rpc-tmpl@latest  # RPC 模板生成器
	go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest  # OpenAPI v2 生成器
	go install github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc@latest  # Protobuf 文档生成器
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest  # Go 代码检查工具
	go install github.com/swaggo/swag/cmd/swag@v1.8.12               # Swagger 文档生成器
	go install github.com/ofabry/go-callvis@latest                   # Go 调用关系可视化
	go install golang.org/x/pkgsite/cmd/pkgsite@latest               # Go 包文档查看器
# delete the templates code end


.PHONY: ci-lint
# 代码质量检查：格式化、命名规范、安全性、可维护性等（规则在 .golangci.yml 中定义）
ci-lint:
	@gofmt -s -w .
	golangci-lint run ./...


.PHONY: test
# 运行单元测试，-count=1 参数禁用缓存以确保每次都执行测试
test:
	go test -count=1 -short ${PKG_LIST}


.PHONY: cover
# 生成测试覆盖率报告并生成 HTML 可视化页面
cover:
	go test -short -coverprofile=cover.out -covermode=atomic ${PKG_LIST}
	go tool cover -html=cover.out


.PHONY: graph
# 生成交互式函数依赖关系图（SVG 格式）
graph:
	@echo "generating graph ......"
	@cp -f cmd/serverNameExample_mixExample/main.go .
	go-callvis -skipbrowser -format=svg -nostd -file=serverNameExample_mixExample github.com/18721889353/sunshine
	@rm -f main.go serverNameExample_mixExample.gv

# delete the templates code start
.PHONY: docs
# 生成 Swagger API 文档，仅适用于基于 SQL 创建的 Web 服务
docs:
	@bash scripts/swag-docs.sh $(HOST)
# delete the templates code end

.PHONY: proto
# 从 Proto 文件生成 Go 代码和模板，默认处理 api 目录下所有 proto 文件，可指定特定文件（多个用逗号分隔）
# 用法：make proto FILES=api/user/v1/user.proto
proto:
	@# Check if go.mod has replace directive for sunshine
	@if grep -q "replace.*sunshine =>" go.mod 2>/dev/null; then \
		SUNSHINE_SRC=$$(grep "replace.*sunshine =>" go.mod | sed 's/.*=> //' | tr -d ' '); \
		if [ -d "$$SUNSHINE_SRC" ]; then \
			echo "Detected local sunshine source: $$SUNSHINE_SRC"; \
			echo "Rebuilding sunshine command and protoc plugins from local source..."; \
			(cd "$$SUNSHINE_SRC" && go install ./cmd/sunshine ./cmd/protoc-gen-go-gin ./cmd/protoc-gen-go-rpc-tmpl); \
			echo "Sunshine command and protoc plugins updated successfully!"; \
		else \
			echo "Warning: sunshine source directory not found: $$SUNSHINE_SRC"; \
		fi \
	else \
		echo "No local sunshine replace found, ensuring remote tools are installed..."; \
		which sunshine >/dev/null 2>&1 || go install github.com/18721889353/sunshine/cmd/sunshine@latest; \
		which protoc-gen-go-gin >/dev/null 2>&1 || go install github.com/18721889353/sunshine/cmd/protoc-gen-go-gin@latest; \
		which protoc-gen-go-rpc-tmpl >/dev/null 2>&1 || go install github.com/18721889353/sunshine/cmd/protoc-gen-go-rpc-tmpl@latest; \
		echo "Remote tools ready."; \
	fi
	@bash scripts/protoc.sh $(FILES)  # 执行 proto 代码生成脚本
	go mod tidy  # 整理依赖
	@gofmt -s -w .  # 格式化生成的代码


.PHONY: proto-doc
# 从 Proto 文件生成 Markdown 格式的 API 文档
proto-doc:
	@bash scripts/proto-doc.sh


.PHONY: build
# 编译 serverNameExample_mixExample 为 Linux AMD64 平台的可执行文件（输出到 cmd/serverNameExample_mixExample 目录）
build:
	@echo "building 'serverNameExample_mixExample', linux binary file will output to 'cmd/serverNameExample_mixExample'"
	@cd cmd/serverNameExample_mixExample && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build

# delete the templates code start

.PHONY: build-sunshine
# 编译 sunshine 命令行工具为 Linux AMD64 平台的可执行文件（去除调试信息以减小体积）
build-sunshine:
	@echo "building 'sunshine', linux binary file will output to 'cmd/sunshine'"
	@cd cmd/sunshine && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "all=-s -w"

.PHONY: image-build-sunshine
# 构建 sunshine Docker 镜像，需指定 TAG 版本号，例如：make image-build-sunshine TAG=v1.5.8
image-build-sunshine:
	@echo "build a sunshine docker image'"
	@cd cmd/sunshine/scripts && bash build-sunshine-image.sh  $(TAG)

# delete the templates code end

.PHONY: run
# 构建并运行服务，可指定配置文件路径
run:
	@bash scripts/run.sh $(CONFIGFILE)


.PHONY: run-nohup
# 后台运行服务（使用 nohup），如需停止服务则传递 CMD=stop 参数，例如：make run-nohup CMD=stop
run-nohup:
	@bash scripts/run-nohup.sh $(CMD)


.PHONY: run-docker
# 在本地 Docker 中运行服务，如需更新服务则重新执行此命令（依赖 image-build-local）
run-docker: image-build-local
	@bash scripts/deploy-docker.sh


.PHONY: binary-package
# 将编译好的二进制文件打包成 tar.gz 压缩包（用于部署）
binary-package: build
	@bash scripts/binary-package.sh


.PHONY: deploy-binary
# 部署二进制文件到远程 Linux 服务器，需提供用户名、密码和 IP 地址，例如：make deploy-binary USER=root PWD=123456 IP=192.168.1.10
deploy-binary: binary-package
	@expect scripts/deploy-binary.sh $(USER) $(PWD) $(IP)


.PHONY: image-build-local
# 为本地 Docker 构建镜像（tag=latest），使用编译好的二进制文件进行构建
image-build-local: build
	@bash scripts/image-build-local.sh


.PHONY: image-build
# 为远程仓库构建 Docker 镜像，使用二进制文件构建，例如：make image-build REPO_HOST=addr TAG=latest
image-build:
	@bash scripts/image-build.sh $(REPO_HOST) $(TAG)


.PHONY: image-build2
# 为远程仓库构建 Docker 镜像（两阶段构建方式），例如：make image-build2 REPO_HOST=addr TAG=latest
image-build2:
	@bash scripts/image-build2.sh $(REPO_HOST) $(TAG)


.PHONY: image-push
# 推送 Docker 镜像到远程仓库，例如：make image-push REPO_HOST=addr TAG=latest
image-push:
	@bash scripts/image-push.sh $(REPO_HOST) $(TAG)


.PHONY: deploy-k8s
# 部署服务到 Kubernetes 集群（使用 kubectl 应用 YAML 配置）
deploy-k8s:
	@bash scripts/deploy-k8s.sh


.PHONY: image-build-rpc-test
# 构建 gRPC 测试镜像并推送到远程仓库，例如：make image-build-rpc-test REPO_HOST=addr TAG=latest
image-build-rpc-test:
	@bash scripts/image-rpc-test.sh $(REPO_HOST) $(TAG)


.PHONY: patch
# 补充部分依赖代码，例如：
#   make patch TYPE=types-pb          # 生成 types.pb.go 相关代码
#   make patch TYPE=init-mysql        # 初始化 MySQL 数据库驱动代码（支持 mysql、redis、rabbitmq 等）
patch:
	@bash scripts/patch.sh $(TYPE)


.PHONY: copy-proto
# 从 gRPC 服务器目录复制 proto 文件，多个目录或文件用逗号分隔，默认复制所有 proto 文件
# 用法示例：
#   make copy-proto SERVER=yourServerDir                                    # 复制所有 proto 文件
#   make copy-proto SERVER=yourServerDir PROTO_FILE=file1.proto,file2.proto # 复制指定的 proto 文件
copy-proto:
	@sunshine patch copy-proto --server-dir=$(SERVER) --proto-file=$(PROTO_FILE)


.PHONY: modify-proto-pkg-name
# 修改 api 目录下所有 proto 文件的 package 和 go_package 名称（统一命名规范）
modify-proto-pkg-name:
	@sunshine patch modify-proto-package --dir=api --server-dir=.


.PHONY: update-config
# 根据 YAML 配置文件自动生成 internal/config 中的 Go 结构体代码（保持配置与代码同步）
update-config:
	@sunshine config --server-dir=.


.PHONY: clean
# 清理生成的文件：二进制文件、测试覆盖率报告、临时文件、自动生成的代码等
clean:
	@rm -vrf cmd/serverNameExample_mixExample/serverNameExample_mixExample*  # 删除编译的二进制文件
	@rm -vrf cover.out                                                        # 删除测试覆盖率报告
	@rm -vrf main.go serverNameExample_mixExample.gv                          # 删除函数依赖图临时文件
	@rm -vrf internal/ecode/*.go.gen*                                         # 删除自动生成的错误码文件
	@rm -vrf internal/routers/*.go.gen*                                       # 删除自动生成的路由文件
	@rm -vrf internal/handler/*.go.gen*                                       # 删除自动生成的 handler 文件
	@rm -vrf internal/service/*.go.gen*                                       # 删除自动生成的 service 文件
	@rm -rf serverNameExample-binary.tar.gz                                   # 删除打包的二进制压缩包
	@echo "clean finished"


# Show help
help:
	@echo ''
	@echo 'Usage:'
	@echo '  make <target>'
	@echo ''
	@echo 'Targets:'
	@awk '/^[a-zA-Z\-_0-9]+:/ { \
	helpMessage = match(lastLine, /^# (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 0, index($$1, ":")-1); \
			helpMessage = substr(lastLine, RSTART + 2, RLENGTH); \
			printf "\033[1;36m  %-22s\033[0m %s\n", helpCommand,helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' $(MAKEFILE_LIST)

.DEFAULT_GOAL := all
