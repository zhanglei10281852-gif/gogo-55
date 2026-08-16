# BENZHI_README

## 项目说明

- 项目：zhanglei10281852-gif/gogo-55
- 项目用途：ReleaseRail is an original, standard-library-only Go CLI and backend for planning and simulating release orchestration entirely offline. It validates strict JSON or a deliberately limited YAML subset, checks semantic dependency constraints and local artifacts, builds deterministic rollout waves, evaluates gates and health criteria, orders migrations, simulates apply/rollback transitions, persists recoverable state, and maintains a verifiable append-only audit hash chain.
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/releaserail

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-55-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-55-arm64 linux/arm64
docker run -it benzhi-task-55-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-55-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/rail -run "^TestRecoverRecordsAuditEntry$" -count=1 -v`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
