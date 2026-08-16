# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

把发布状态恢复到较早的快照之后，状态确实回退了，但审计日志里完全看不到这次恢复：记录条数没变、校验仍然说链是有效的，事后审计只能看到那些已经被回退掉的操作。先不要修改代码。请调查恢复流程为什么不会在审计链中体现，给出可核验证据、完整因果链，并定位具体 Go 文件和符号。

## 含 Bug 版本

- 仓库：zhanglei10281852-gif/gogo-55
- 仓库地址：https://github.com/zhanglei10281852-gif/gogo-55.git
- parent SHA：d85dd640d45441cf0381e905b34434cbc083b72d

## 复现步骤

```bash
git clone -- https://github.com/zhanglei10281852-gif/gogo-55.git bug-repro
cd bug-repro
git checkout --detach d85dd640d45441cf0381e905b34434cbc083b72d
go test ./internal/rail -run "^TestRecoverRecordsAuditEntry$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/rail -run "^TestRecoverRecordsAuditEntry$" -count=1 -v
=== RUN   TestRecoverRecordsAuditEntry
    recover_audit_regression_test.go:48: recovery left no audit record: records went from 1 to 1
--- FAIL: TestRecoverRecordsAuditEntry (0.04s)
FAIL
FAIL	releaserail/internal/rail	0.041s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/rail -run "^TestRecoverRecordsAuditEntry$" -count=1 -v
=== RUN   TestRecoverRecordsAuditEntry
    recover_audit_regression_test.go:48: recovery left no audit record: records went from 1 to 1
--- FAIL: TestRecoverRecordsAuditEntry (0.14s)
FAIL
FAIL	releaserail/internal/rail	0.263s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

定位 internal/rail/store.go 的 (*Store).Recover，并结合 (Store).Save、(Store).AppendAudit、(Store).VerifyAudit 说明恢复为何不进入审计链；解释审计校验仍报有效的完整机制；有证据且目标仓库零改动。
