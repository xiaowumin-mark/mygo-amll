# 初始化验证记录

日期：2026-11-09。验证时对应尚未提交或发布的初始化工作区。记录用于界定已验证范围，不是原生渲染性能承诺。

## 核心检查

本机 Windows/amd64、Go 1.27.3、Python 3.13.16，GCC race 工具链可用。

- `python scripts/check.py`：gofmt、vet、race、Python 检查器测试与 `git diff --check` 通过。
- `go run ./examples/timeline`：输出 `A new morning` 和 `A second voice`，演示同时活动的两行。
- 核心单独审阅并验证了输入快照、半开区间、重叠/seek、随机线性对照和独立缓冲区的并发查询。
- 工作流通过 actionlint v1.7.7 静态检查；release JSON 语法和本地 Markdown 文件链接检查通过。

没有推送到远程，也没有在 GitHub runner 上执行本次新增的工作流。仓库设置、GitHub App 凭证及 ruleset 仍需按 [发版指南](releases.md) 启用并验证。

## 时间索引基准

命令：`go test ./... -run '^$' -bench . -benchmem -count 3`。

CPU：11th Gen Intel Core i5-1135G7 @ 2.40 GHz，Go 基准后缀为 `-8`。没有固定 CPU 频率/电源模式；以下仅为本机初始参考。每组是 3 次运行的中位数，所有组均为 **0 B/op、0 allocs/op**。

| 行数 | 常规区间 ns/op | 首行覆盖全曲 ns/op |
| --- | ---: | ---: |
| 100 | 15.79 | 53.93 |
| 1,000 | 43.54 | 406.4 |
| 10,000 | 71.78 | 6,961 |

基准只查询时间区间，不包含文本、shaping、绘制、输入解析和索引构造。输出缓冲区预分配，查询位置轮转覆盖全曲。长重叠组反映候选扫描最坏趋近 O(n) 的成本，不能据常规组数字宣称所有输入查询都为 O(log n)。

没有测量峰值 heap、RSS、GPU 纹理或窗口帧间隔；`0 B/op` 只描述该查询操作，不能代表整库或整进程零内存成本。

## mygo 技术可行性

在临时独立模块，以固定上游 commit、`CGO_ENABLED=0` 完成 headless CPU 冒烟验证：Native Content 类型、CJK/RTL 整行文字、glyph 裁剪、Canvas、shader Values 与像素输出。具体版本和边界见 [上游依据](upstreams.md#验证范围与待验证项)。临时模块不是本仓交付物；后续 M1 必须把可重复的原生示例和测试正式加入仓库。

尚未验证真实 Windows GPU 窗口、macOS、Linux、动态效果、设备资源回收、可访问性与 60/120 Hz。它们是后续里程碑的验收项。
