# ==============================================================================
# 🛠️ Terminus Build System
# ==============================================================================

# --- 变量定义 ---
BINARY_NAME=terminus-enforcer
SCHEDULER_BIN_NAME=terminus-scheduler
INJECTOR_BIN_NAME=terminus-quota-injector
CMD_PATH=./cmd/terminus-enforcer
SCHEDULER_PATH=./cmd/terminus-scheduler
INJECTOR_PATH=./cmd/terminus-quota-injector
BIN_DIR=./bin
DOCKER_IMAGE=terminus-enforcer
VERSION?=v0.2.0

GIT_COMMIT=$(shell git rev-parse --short HEAD || echo "unknown")
BUILD_TIME=$(shell date "+%F %T")
LDFLAGS=-ldflags "-s -w -X 'main.Version=${VERSION}' -X 'main.GitCommit=${GIT_COMMIT}' -X 'main.BuildTime=${BUILD_TIME}'"

# --- 默认任务 ---
.PHONY: all
all: build

# ==============================================================================
# 📦 编译与构建 (Build)
# ==============================================================================

.PHONY: build
build: ## 编译当前平台的二进制文件
	@echo "🚀 Building ${BINARY_NAME}..."
	@mkdir -p ${BIN_DIR}
	go build -ldflags '-s -w' -o ${BIN_DIR}/${BINARY_NAME} ${CMD_PATH}
	@echo "✅ Build success: ${BIN_DIR}/${BINARY_NAME}"

.PHONY: build-scheduler
build-scheduler:
	@echo "🚀 Building ${SCHEDULER_BIN_NAME}..."
	@mkdir -p ${BIN_DIR}
	go build ${LDFLAGS} -o ${BIN_DIR}/${SCHEDULER_BIN_NAME} ${SCHEDULER_PATH}
	@echo "✅ Build success: ${BIN_DIR}/${SCHEDULER_BIN_NAME}"

.PHONY: build-quota-injector
build-quota-injector:
	go build -o ${BIN_DIR}/${INJECTOR_BIN_NAME}  ${INJECTOR_PATH}



.PHONY:  build-linux-scheduler
build-linux-scheduler:
	@echo "🐧 Building Linux amd64  scheduler static binary..."
	@mkdir -p ${BIN_DIR}
	CGO_ENABLED=0 GOOS=linux  go build -a -ldflags '-s -w -extldflags "-static"' -o ${BIN_DIR}/${SCHEDULER_BIN_NAME}-linux ${SCHEDULER_PATH}
	@echo "✅ Linux binary ready: ${BIN_DIR}/${SCHEDULER_BIN_NAME}-linux"


.PHONY: build-linux-quota
build-linux-quota:
	@mkdir -p ${BIN_DIR}
	CGO_ENABLED=0 GOOS=linux  go build -a -ldflags '-s -w -extldflags "-static"' -o ${BIN_DIR}/${INJECTOR_BIN_NAME}-linux ${INJECTOR_PATH}
	@echo "✅ Linux binary ready: ${BIN_DIR}/${INJECTOR_BIN_NAME}-linux"

.PHONY: build-linux
build-linux:
	@echo "🐧 Building Linux amd64  static binary..."
	@mkdir -p ${BIN_DIR}
	CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-s -w -extldflags "-static"' -o ${BIN_DIR}/${BINARY_NAME}-linux ${CMD_PATH}
	@echo "✅ Linux binary ready: ${BIN_DIR}/${BINARY_NAME}-linux"

.PHONY: run
run: build
	@echo "🏃 Running ${BINARY_NAME}..."
	sudo ${BIN_DIR}/${BINARY_NAME} --v=2

# ==============================================================================
# 🧹 代码质量与清理 (Quality & Clean)
# ==============================================================================

.PHONY: clean
clean: ## 清理构建产物
	@echo "🧹 Cleaning up..."
	@rm -rf ${BIN_DIR}
	@echo "✅ Done."

.PHONY: fmt
fmt: ## 格式化代码 (go fmt)
	@go fmt ./...

.PHONY: vet
vet: ## 静态检查 (go vet)
	@go vet ./...

.PHONY: lint
lint: ## 运行 golangci-lint (需要先安装)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "⚠️ golangci-lint not installed. Skipping."; \
	fi

.PHONY: test
test: ## 运行单元测试
	@go test -v ./...

# ==============================================================================
# 🐳 Docker 相关
# ==============================================================================

.PHONY: docker
docker: build-linux ## 构建 Docker 镜像
	@echo "🐳 Building Docker image: ${DOCKER_IMAGE}:${VERSION}"
	docker build -t ${DOCKER_IMAGE}:${VERSION} .

# ==============================================================================
# ❓ 帮助信息
# ==============================================================================

.PHONY: help
help: ## 显示帮助信息
	@echo "Terminus Makefile Commands:"
	@awk 'BEGIN {FS = ":.*##"; printf "\033[36m\033[0m"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)