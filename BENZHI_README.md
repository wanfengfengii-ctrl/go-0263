基于 Go 实现的橄榄鲜果冷榨前联检 Web 项目，一款后端服务，管理收样拆分、成熟度计数、氧化核验与冷榨裁决。
# olivepress-fruit-intake-gate

本 Git 项目来自模型完成任务后的 workspace，不包含嵌套 .git 记录或本地构建产物。

## 本地构建与测试

```bash
go mod download
go build ./...
go test ./...
./run_benzhi_smoke.sh
```

## Docker 构建与运行

```bash
./build_benzhi_docker.sh olivepress-fruit-intake-gate linux/arm64
docker run --rm -it --platform linux/arm64 olivepress-fruit-intake-gate:latest
./build_benzhi_docker.sh olivepress-fruit-intake-gate linux/amd64
docker run --rm -it --platform linux/amd64 olivepress-fruit-intake-gate:latest
```

构建脚本第二个参数为目标平台，必须分别完成 linux/arm64 和 linux/amd64 构建与容器验证；未提供时按照规范默认使用 linux/amd64。系统 backend-v2 模板通过 Go 原生交叉编译生成目标架构的 /usr/local/bin/benzhi-app，镜像默认直接运行该入口。
