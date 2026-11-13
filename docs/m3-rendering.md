# M3 渲染进展与基础原型历史记录

日期：2026-11-13。状态：**所有者已签收当前 M3 视觉实现，并授权推送、合并。** 本记录保留原型拒收历史与性能验证边界；CPU 峰值归因、逐屏幕呈现间隔和其他平台原生窗口仍未完整测量，不据此宣称无掉帧或跨平台性能通过。当前参考路径及连续动画修复证据见下文与[一致性审计](m3-amll-parity.md)。设计取舍见 [RFC 0003](rfcs/0003-m3-rendering.md)。

2026-11-13 修复三个点缩放的设备像素阶跃：固定圆点几何通过 `Painter.Transform` 应用已有 Pose 缩放；动画值、时序、透明度和 CSS 盒舍入未变。所有者已签收视觉；最大化窗口性能数据与逐屏幕呈现测量限制见 [M3 无损绘制优化](m3-performance.md)。

固定源码及真实原库运行揭示的差异见 [AMLL 一致性审计](m3-amll-parity.md)。原有截图只比较 Native 自身重复结果，没有与 AMLL 对照；不能称为高保真截图验收。

2026-11-10新增的参考路径已成为普通示例默认路径，设计见[RFC 0004](rfcs/0004-m3-native-parity.md)，实际对照方法见[原库验证](m3-parity-validation.md)。下文“旧原型”章节保留被否决版本的实现和测量，**不能套用到当前高保真路径**。当前参考路径使用不同的词盒、合成、参数、缓存和逐帧调度；尚未完成全部平台/效果/性能验收。

## 2026-11-11 连续动画修复

滚动与文字上浮的整像素阶跃已定位并修复：固定 mygo 的 `Shader.Draw` 会先对目的矩形做物理像素取整。原实现直接把动画小数坐标作为矩形原点，即使纹理已经双线性采样，缓慢位移仍会先停住、再跳一格。现在所有词、行、组与静态 atlas 的 shader 绘制都使用局部原点，通过公开 `Painter.Transform` 的平移保留小数位置。没有修改参考弹簧、时序、原库的 0.1 DIP/0.001 舍入、依赖版本或缓存预算。

另移除了参考路径的 paint-only 定时重绘；其冻结时间戳只能由完整 view 更新推进。动态路径在 idle 输入、resize 或同尺寸 DPI/布局变化后保留一次完整确认帧，使 paint 中首次发现的新布局能继续动画；暂停收敛、隐藏、静态、减少动态效果和软件降级不因此持续请求帧。

新增回归覆盖 X/Y 亚像素位移，1×/1.25×/2× DPI，60/120 Hz 主词与背景词上浮，父级平移/缩放，以及暂停 resize、同尺寸布局和 DPI 变化后的唤醒与停止。真实 Windows D3D 屏幕捕获也重现了修复前的阶跃、确认了修复后的连续位移；所有者已签收当前视觉实现。数值和性能测量边界见[审计记录](m3-amll-parity.md)。

## 运行与验收

2026-11-12 新增内容/水平模糊复用、完整高斯核预计算、字符参数前缀复制和旧 Canvas 依赖释放，帧率、分辨率、模糊半径及动画参数不变；前后测量、逐像素证据和资源边界见 [M3 无损绘制优化](m3-performance.md)。2026-11-13 圆点缩放改用固定几何加亚像素变换；视觉已由所有者签收，CPU 峰值和逐屏幕呈现边界仍保留为性能报告限制。

```sh
go run ./examples/native
go run ./examples/native -static
go run ./examples/native -lines 10000 -software
go run ./examples/native -font "Segoe UI, Microsoft YaHei UI, sans-serif"
go run ./examples/native -ttml "/path/to/lyrics.ttml" -at 90s
go run ./examples/native -smoke-duration 12s
python scripts/check.py
```

默认文字均为合成输入，无音频、网络、歌词版权资产或封面。默认依次展示中英日、Arabic/Hebrew、emoji、组合字符、连字与长行，并含翻译、音译、同时开始的对唱、背景词和长间奏。`-ttml` 可由示例宿主读取本地文件，限制为 8 MiB、10,000 物理/输出行，其他限制沿用 M2；只在启动时读文件和解析，不进入绘制路径。`-at` 指定初始歌曲绝对位置，文件和内容不自动复制进仓库。

| 操作 | 行为 |
| --- | --- |
| Space / Play / Pause | 暂停或恢复模拟宿主时钟 |
| 左右方向键 / ±5s | 绝对时间 seek；布局可复用 |
| 滚轮、触控板、鼠标/触摸拖动 | 手动浏览并暂停自动跟随；取消手势不 seek |
| 上下方向键 / PgUp / PgDn | 按行浏览；Enter 请求当前浏览行 seek |
| 点击行正文或注释 | 发送行 ID 和绝对时间，由示例宿主处理 seek |
| Home / Follow | 立即恢复跟随许可，动效模式以弹簧回到焦点 |
| Static lyrics / `-static` | 禁用平滑高亮与滚动，使用确定性词结束阶梯 |
| Font families | 修改字体 fallback 列表并使相关几何失效 |

手动浏览后不会仅因时间到了就抢回。手势结束至少 500 ms，且出现新的、可见的正在演唱焦点时才自动恢复；暂停期间、屏外焦点、拖动期间均不会抢回。显式 Follow 立即恢复，seek 在手势结束后恢复。曲终、间奏、保留高亮和任意重叠采用 `timeline.VisualAt`，不再让所有历史歌词永久亮起。

## 文字与绘制语义（旧原型）

正文整体 shaping，公开 glyph cluster 与 wrapped layout 的字素边界合并，缓存 UTF-8 字节、rune、UTF-16 范围转换结果和多个高亮矩形。RTL 每簇按自己的方向填色；混合方向词保留逻辑簇顺序。跨时间边界的共享连字、组合字符或 ZWJ emoji 等待全部所属词的最晚 End，避免前词结束就染亮后词。无词行按整行区间，静态路径在词结束时更新。

词与正文必须按顺序精确匹配，允许跳过未计时空白以支持 TTML span 间空格；不猜测或跳过非空白文本。翻译与行音译分别整行布局在正文下，参与实际高度与命中区域；对唱靠右，背景词字号为正文的 0.8 倍，注释为其正文的 0.6 倍。未实现逐词音译对齐、ruby、模糊与完整 AMLL 强调效果，也不承诺不同平台逐像素一致。

字体 fallback 使用固定 mygo 的公开系统字体机制。`Language` 优先于宿主默认 `Font.Lang`；同名字体数据被宿主重载时须在 UI 线程调用示例内的 `invalidateGeometry`。系统缺少 CJK/emoji 字体时无法保证字形完整，测试不将正宽度当作“所有字符都有正确字形”的证据。

## 帧准备、预算与释放（旧原型）

| 所有者 / 数据 | 上限与成本 | 失效 / 释放 |
| --- | --- | --- |
| scene 文档、时间索引、两个查询缓冲与排序事件表 | O(行数 + 词数)，示例最多 10,000 行；VisualAt 长重叠最坏 O(n) | 换曲应卸载并 Close 旧 scene |
| geometryCache | 32 条目、16 MiB 估算；本帧固定布局纳入同一预算 | 文档、字体、4 DIP 宽度桶、Scale 改变失效；LRU 淘汰 |
| 本帧布局引用 | 最多 32 行；可见内容优先，上下至多一视口 overscan | 准备窗口改变时解除旧固定，Close 清空 |
| 正文加音译/翻译 | 单行总计 16 KiB UTF-8、512 时间词 | 超限或映射错误使用固定可读提示 |
| 行高 segment tree | 初始化 O(n)，定位 / 更新 O(log n)；10,000 行约 256 KiB 标量存储 | 文档 / 字体 / 桶宽 / DPI 改变重建；Close 清空 |
| 高亮工作区 | 按受限单行的最大矩形数冷预分配并复用 | Close 清空 |
| 解析弹簧 | O(1) 时间与空间；显式时间纯查询 | seek / 恢复可见 / 静态 / 减少动态效果直接定位 |

只准备锚点附近的实际行，未见行使用估算高度，不对全曲 shaping。分数行滚动避免上方行高细化使锚点跳动；快速拖到未见行有受限冷布局成本。32 行硬上限优先于超大视口的 overscan；若可见行本身超过上限，极端视口可能留空，不承诺任意窗口高度的完整覆盖。4 DIP 向下分桶不会借用宽于实际可用空间的布局；上游返回的布局总宽度可能包含越过断行宽度的行尾空白，测试另行检查非空白文字区域。

业务变更只在 view / input，Draw 仅读锚点和准备缓存。行起止事件才请求 view 更新；滚动与词内动画用仅重绘帧。软件、减少动态效果和静态模式不请求连续动画。隐藏时停止工作，恢复按宿主锚点重新查询；Close 幂等，解除索引、文档、行高、几何、工作区和宿主 seek 回调，无隐式 goroutine。上述字节估算不包括上游字体缓存、驱动或 GPU 图集，不能当作 RSS 上限。

## 自动化与截图口径

```sh
go test ./examples/native ./internal/layout ./internal/motion -count=1
go test ./examples/native -run TestM3Snapshot -count=1 -v
go test ./examples/native ./internal/layout ./internal/motion -run '^$' -bench . -benchmem -count 5
```

设置 `MYGO_AMLL_SNAPSHOT_DIR` 为系统临时目录可保留 overview、窄窗口、高 DPI、RTL 与字体替换 PNG。未设置时放在测试临时目录，不将生成截图提交仓库。截图固定暂停位置、尺寸与静态模式，通过已有 x/image 的 GoRegular/GoBold 固定 Latin 字体；CJK/RTL/emoji 仍使用当前平台 fallback。相同状态重复绘制与新建独立 scene 的像素容差为 0；这验证同机确定性，不是已建立跨平台像素金图。

几何测试覆盖实际 mixed RTL、组合字符、ZWJ emoji、多矩形、短至 1 ns 的时间词、共享簇、注释、背景、空白映射、字体回退、超限与缓存固定预算。行高测试使用随机更新和线性参照；弹簧使用解析轨迹、重定向及不同采样频率。真实 Tester 输入覆盖点击/拖动/取消、键盘、宿主不处理 seek、手势中外部 seek、恢复跟随、暂停调度与暖态零分配。真实窗口与 GPU 仍须单独记录。

`MYGO_AMLL_TEST_TTML` 指向用户提供的本地文件时，`go test ./examples/native -run TestM3ExternalTTML -count=1 -v` 追加实际 TTML 的解析、全行几何与前后 seek 检查，并可输出本地渲染截图。默认 CI 跳过此测试；外部文件不成为分发 fixture，不能把本地文件测试当作 CI 可复现证据。

本轮实际文件声明 Apple `timing="Word"`。profile 用于解释裸秒；`body/div` 时间忽略，段落和词时间按 AMLL 方式直接使用，并由词/背景区间扩展段落范围。实际输出为 84 行、567 时间词、84 行翻译、0 行音译、36 对唱、14 背景，26 行与之前区间重叠。320 DIP / 0.75×、720 / 1×、1100 / 2× 三组逐行正反 seek，共 504 次准备检查通过；缓存峰值 32 条 / 2,626,528 B，帧固定峰值 29 条 / 2,334,848 B。iTunesMetadata 逐词 sidecar 以一条诊断跳过，不计入当前模型。此记录不证明所有 Apple TTML 兼容。

## CPU 基准与原生验证

代码基于 `24d07407585941422bc99cb5f267a21f24e86a08` 的未提交工作区。环境：Windows 11 Pro 10.0.26300、Go 1.27.3 windows/amd64、Python 3.13.16、GCC 16.1.0、Intel i5-1135G7，基准后缀 `-8`。Intel Iris Xe 驱动 31.0.101.4255，另有 GameViewer Virtual Display Adapter 15.6.5.199。未锁定 CPU 频率/电源模式，部分前测与编译工作重叠，不据纳秒差异作统计显著的性能判断。

最终命令：

```sh
go test ./examples/native ./internal/layout ./internal/motion -run '^$' -bench 'BenchmarkCachedHighlights|BenchmarkClusterHighlights|BenchmarkLineGeometry|BenchmarkScenePreparation|BenchmarkHeights|BenchmarkSpringSample' -benchmem -benchtime=300ms -count 5
```

以下为 5 次中位数；除冷几何一行外，全部 **0 B/op、0 allocs/op**。

| 操作 | M3 前 ns/op | 最终 ns/op（5 次范围） |
| --- | ---: | ---: |
| CachedHighlights：兼容旧合成词矩形路径 | 8.669 | 12.35（11.97–12.51） |
| ScenePreparation / 200 行 | 11.86 | 74.60（71.80–75.51） |
| ScenePreparation / 2,000 行 | 15.81 | 83.18（78.80–95.79） |
| ScenePreparation / 10,000 行 | 19.77 | 87.24（83.10–91.28） |
| ClusterHighlights：实际混排/RTL/emoji/换行 | 无对应实现 | 346.4（332.2–357.5） |
| Heights / 10,000 行定位查询 | 无对应实现 | 84.70（84.28–85.52） |
| Heights / 10,000 行实际高度更新 | 无对应实现 | 45.45（45.04–45.76） |
| SpringSample | 无对应实现 | 29.95（29.60–30.27） |
| LineGeometry：带两条注释的冷整行布局 | 无对应实现 | 273,642（271,389–303,329）；约 61,261 B/op、519 allocs/op |

旧准备的 5 次原始值分别为 200 行 `12.94/11.67/11.36/11.88/11.86`、2,000 行 `15.26/14.70/15.81/17.82/19.69`、10,000 行 `19.77/19.81/20.90/17.97/17.06`；旧高亮为 `9.090/8.345/8.669/8.254/10.35`。前后均复用调用方缓冲。旧版本仅以最后开始时间定位，M3 准备包含 M2 的重叠/保留高亮/间奏语义，默认 fixture 也新增注释；两者工作量不同，不能当作同等视觉功能的加速/回归比较。兼容旧矩形路径只是小型算法对照，实际 M3 使用簇级路径。冷布局、实际绘制提交和全帧成本不能用暖态命中代替。

最终原始输出位于本机仓库外 `%TEMP%/mygo-amll-m3-run-20261109/bench-complete-final.txt`，生成截图和日志也仅存系统临时目录。

| 验证 | 本轮结果与边界 |
| --- | --- |
| `python scripts/check.py` | 最终 gofmt、vet、全包 race、Python 标题检查器测试与 diff 空白检查通过；包含用户 TTML 的 opt-in 测试 |
| 依赖 | `go mod verify` 通过；mygo 版本未改，x/image 仅因截图字体从间接依赖变为直接依赖，无新版本 |
| CPU 截图 | 六种固定场景及快速 resize 往返通过，同状态重复/独立 scene 像素差为 0；非 Latin 系统 fallback 的限制保留 |
| 用户 TTML | 全行正反 seek、三组宽/DPI、预算与注释检查通过；背景/对唱/尾行截图已检查，非分发 fixture |
| Windows 默认真实窗口 | 用户 TTML 的 12 秒 smoke 退出 0；Accelerated=true、Scale=1，实际经过暂停/seek、resize、Hide/Show、2×、关闭；740×580 窗口截图已检查 |
| Windows 软件真实窗口 | 相同文件/序列退出 0；Accelerated=false、Scale=1，文字、翻译、背景词可读，窗口截图已检查 |
| Windows / Linux amd64 / macOS arm64 构建 | Native 示例构建通过，Linux/macOS 使用 CGO_ENABLED=0 交叉构建；不等于运行 |
| macOS/Linux 真实窗口、跨屏 DPI、系统遮挡、读屏器与多窗口 | **未测**，不由 CPU Tester 或 Windows 代签 |
| 呈现帧间隔、GPU 完成耗时、CPU p95/p99、RSS/长时内存趋势 | **未测**，不宣称达到性能目标 |

用户文件默认路径最后记录 draws=218、view-builds=10，软件路径 draws=52、view-builds=9；均缓存 16 条、估算 513,264 B。这些是示例调用计数和缓存估算，包含暂停与隐藏，不能换算为呈现帧率、进程内存或 GPU 内存。

所有者于 2026-11-13 确认当前视觉实现并授权提交、推送及合并。远程 PR 的检查和合并状态以 GitHub 记录为准；未实测的平台和逐屏幕呈现指标仍按未测处理。
