# 开发规则

## 环境与命令

Go 最低 1.27.3，与当前 mygo 快照一致；Python 最低 3.11 用于零第三方依赖的仓库检查脚本。主模块为 M1 示例固定 mygo 并提交 `go.sum`；核心包 import 图仍只有标准库和本库。运行核心不需要图形环境，原生示例的平台依赖见 [M1 说明](m1-native.md)。无需 `go install` 或 npm。

| 任务 | 命令 |
| --- | --- |
| 完整本地检查 | `python scripts/check.py` |
| 缺少 race 工具链时的部分检查 | `python scripts/check.py --no-race` |
| 格式化 | `gofmt -w lyric timeline examples` |
| 测试 | `go test ./...` |
| 并发检查 | `go test -race ./...` |
| 静态检查 | `go vet ./...` |
| 基准 | `go test ./... -run '^$' -bench . -benchmem -count 5` |
| 示例 | `go run ./examples/timeline` |
| 原生示例 | `go run ./examples/native`、`go run ./examples/native -software -smoke` |
| 工作区/空白检查 | `git status --short`、`git diff --check` |
| 提交标题检查 | `python scripts/check_pr_title.py "feat(timeline): add seek state"` |

`-race` 是 Go 的动态并发检测工具，需要 `CGO_ENABLED=1` 和匹配的 C 编译器；这不表示 mygo Native UI 依赖 cgo。Linux/macOS CI 使用 runner 的编译器，Windows 使用受支持的 GCC 工具链。`--no-race` 不代替合并前的 CI 检查。

## 编码和接口

- UTF-8、LF、`gofmt`；YAML 和 Markdown 嵌套列表使用两空格。`.editorconfig` 规定通用编辑约定。
- 采用 Go 包结构和标准库测试，避免没有实际扩展需求的层级、接口或空包。核心不依赖 UI、音频、网络、全局日志配置或播放器服务。
- 公共符号写 GoDoc，说明所有权、时间单位、线程规则和返回缓冲区寿命。错误须含有可定位的上下文，不返回静默修复后的数据。
- 文档必须区分已实现、提案和测量目标；未发布 API 不写成安装后的功能。不要编造 mygo 生命周期或 GPU 能力。
- 渲染资源只通过上游公开 API 操作；禁止导入 `internal/*`、使用 `unsafe` 读取布局或依赖未声明的全局状态。确需低层例外时先写设计提案和测量证据。

## 性能与并发

设计改动先说明复杂度和分配，热路径用基准验证。循环复用切片必须约定所有者；公开只读数据不泄漏可变底层切片；对象池和缓存必须有上限、失效和释放引用路径。

后台任务要支持取消与歌曲代次检查，不向已经销毁的 UI 提交旧结果。不得在持锁时调用宿主回调，线程安全不能从单一公开 API 的保证扩展到所有图形对象。数据准备、时间状态变更与 Draw 分离，详见 [性能规则](performance.md)。

## 测试规则

使用表驱动测试和确定性合成 fixture；时间单位、半开区间、空输入、相同开始与任意重叠必须覆盖。跨平台测试不硬编码本机路径或字体。只验证有意义的行为和风险，不为易逆低影响文字变更添加镜像实现的测试。

优化 PR 至少提供固定输入与基准前后原始输出，并确认视觉语义没有退化。Native UI 加入后，真实窗口性能、GPU/软件回退、挂载销毁、DPI 与无障碍需要独立验证；headless 截图不能替代这些记录。

## 依赖与工作流

新增依赖说明必要性、许可证、传递依赖与平台影响。mygo 在实际加入适配器时按固定 commit 锁定，并同步上游快照文档。版本升级用独立 PR，避免与行为重构混合。

Actions 固定完整 commit SHA，PR 工作流只读权限；禁止让 fork PR 通过高权限 `pull_request_target` 执行代码。发布权限仅授予受信默认分支的发版工作流。不要提交凭证、真实账户配置或本地 `ref/`。

## CodeGraph

仓库存在 `.codegraph/` 时，先使用 `codegraph_explore` 或 `codegraph explore` 理解和定位代码；结果不覆盖配置/文档或新文件时再做必要的原始读取。没有索引时跳过，不自动创建或重新索引。参考仓与上游是独立项目，不能假定根索引已包含其源码。
