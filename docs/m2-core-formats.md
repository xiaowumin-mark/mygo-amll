# M2：歌词核心与格式验收

日期：2026-11-09。状态：所有者已确认完整 M2 验收通过，并授权提交合并；各格式按独立 PR 交付，远程检查结果以对应 PR 为准。尚未发布版本。设计讨论与取舍见 [RFC 0002](rfcs/0002-m2-lyric-formats.md)。本轮不改变 M1 原生示例，不将纯 Go 测试作为窗口/GPU 验收。

2026-11-11 追加：所有者要求以完整数据库复核 M2。当前源时间兼容策略与解析扩展见 [RFC 0005](rfcs/0005-m2-corpus-compatibility.md)，固定版本、全量差分、非法源 XML 与检查结果见 [全库记录](m2-corpus-validation.md)。下方基准保留 2026-11-09 历史记录，本次数据单独列示。

## 验收单元

1. **模型与视觉时间**：`lyric` 的稳定行 ID、agent/language 与只读重复元数据；`timeline.VisualAt` 的活动/高亮/焦点/间奏/曲终；`FollowController` 的手动交互与恢复。
2. **LRC/增强 LRC**：`format/lrc`，多时间标签、绝对逐词时间、metadata/offset 与结束推断。
3. **YRC**：`format/yrc`，明文毫秒逐词格式。
4. **QRC**：`format/qrc`，明文后置词时间格式。
5. **TTML**：`format/ttml`，明确限制的歌词 XML 子集。

格式包依赖统一的 `format.Options` / `Result`，共享内部预算与规范化实现。远程提交按核心与 LRC、YRC、QRC、TTML 与集成文档四个独立 PR 依次进行，共同基础先于格式扩展合入。当前没有自动格式探测、文件/网络读取、服务协议解包、序列化或无损回写 API；黄金输入验证的是解析后的语义，不是导出往返。

## 远程交付

所有者于 2026-11-09 在验收后明确授权提交合并，按路线图拆分交付：

- [核心与 LRC：PR #8](https://github.com/xiaowumin-mark/mygo-amll/pull/8) 已 squash 合并。
- [YRC：PR #9](https://github.com/xiaowumin-mark/mygo-amll/pull/9) 已 squash 合并。
- [QRC：PR #10](https://github.com/xiaowumin-mark/mygo-amll/pull/10) 已 squash 合并。
- [TTML 与集成：PR #11](https://github.com/xiaowumin-mark/mygo-amll/pull/11) 已 squash 合并，基线为 `24d0740`。

前三个 PR 均在 Windows/macOS/Linux 四组核心检查（含 Ubuntu stable）、标题检查和基准报告全部通过后合并；检查包含 race 及 Native 示例构建，仍不等于真实窗口/GPU 验收。每个独立提交快照也在本机隔离 worktree 运行了完整检查。未合并 release PR、打标签或发布版本。

## 直接运行

```sh
go run ./examples/formats -format lrc -at 2500ms
go run ./examples/formats -format yrc -at 5s
go run ./examples/formats -format qrc -at 10s
go run ./examples/formats -format ttml -at 12s
go run ./examples/formats -format lrc -input path/to/authorized.lrc -duration 4m -strict
```

不传 `-input` 时四个示例都是合成歌词。输出包含文档、元数据、诊断与视觉状态，JSON 时间数值单位为纳秒（`time.Duration`）。示例第一行在 `[1s,3s)`，第二行在 `[10s,12s)`：2.5s 第一行活动；5s 处于 `[3s,10s)` 的间奏；10s 第二行活动；12s 歌词结束。示例的文件读取属于宿主命令，不进入核心包。

库内使用：

```go
result, err := lrc.Parse(ctx, data, format.Options{Duration: 4 * time.Minute})
if err != nil {
    return err
}
index, err := timeline.New(result.Document)
if err != nil {
    return err
}
playing := make([]int, 0, result.Document.Len())
highlighted := make([]int, 0, result.Document.Len())
state := index.VisualAt(position, playing, highlighted)
playing, highlighted = state.Playing, state.Highlighted
```

两个缓冲区必须使用不同底层存储。后续查询可复用它们，前一次返回切片随之失效；不复用时宿主可自行复制。查询不持有宿主音频时钟，可以直接双向 seek。文档和索引只读，可用独立查询缓冲并发读取。

## 时间、身份与视觉规则

- 所有行和词使用歌曲绝对 `time.Duration`，正常区间为 `[start,end)`。默认文档构造器严格要求正时长、词按开始时间排列且位于行内；格式解析器通过显式 `NewDocumentWithOptions(..., DocumentOptions{PreserveSourceTiming:true})` 保留非负源端点与文本顺序，包含 point、反向、越界和非单调时间。point/反向行永不活动；不交换端点、不按时间重排词。时间溢出与负端点仍失败。LRC 的 offset 仅为 AMLL metadata；YRC/QRC 的 offset 按其支持规则平移时间，TTML 无隐式偏移。
- 行按开始时间稳定排序并产生 `sorted` 诊断；相同开始时间和任意重叠不合并，也不猜测它们是翻译。严格文档构造器继续拒绝未排序输入，排序仅发生在格式层。
- 文档显式 ID 不能重复；没有 ID 的核心行得到确定性的文档局部身份。LRC 使用源行与时间标签序号，YRC/QRC 使用源行，TTML 优先保留 `itunes:key`，兼容 `xml:id` 与生成身份。背景 `ParentID` 必须指向主行。时间查询和排序不改变 ID；自动 ID 不保证跨任意文本编辑稳定。`PropertyCount` / `Property(i)` 返回元数据值，不泄漏切片；Ruby 由构造器深复制，按标量读取时不借出底层切片。
- 已完成行持续高亮到下一次不同开始时间。新行开始时，仍活动的旧行与新行共同组成保留高亮组；短间隔保留该组。间奏和曲终清空高亮。`Focus` 是组中第一行，间奏/曲终保留锚点；`LatestHighlighted` 是当前高亮的最后一行，空时为 -1。
- 间奏要求未演唱间隙至少 **7,000 ms**，使用此前所有行最大结束时间；前奏同样适用，不将负歌曲位置视为间奏。曲终使用全局最大结束时间，不依赖最后开始的行，也不代表宿主音频已结束。空文档 `Focus=-1`、`Ended=false`。
- 手动交互期间不应用自动焦点，但活动与高亮继续更新。`EndInteraction` 后静止至少 **500 ms**，还须出现新的、可见且正在演唱的焦点才自动恢复；单纯超时不抢回。`Resume` 立即恢复；`Seek` 在没有进行中的手势时恢复。交互时间是宿主提供的同一起点单调时间，不能使用会倒退的歌曲位置。换曲用 `Resume` 清除焦点历史。

## 输入限制、取消与诊断

| `format.Limits` 字段 | 默认值 | 计数范围 |
| --- | ---: | --- |
| `MaxBytes` | 8 MiB | 原始输入，含 BOM |
| `MaxTextBytes` | 16 MiB | 展开后的源 ID、行/词文本、注释、agent、language 与元数据字节总和，重复文本分别计入，复制字符串前检查；生成 ID 由行数单独约束 |
| `MaxLines` | 100,000 | 物理输入行和展开后的输出行分别受限；末尾换行不额外计一行 |
| `MaxWords` | 1,000,000 | 时间词、Ruby 与 sidecar 时间段；未选语言也计数，多时间标签复制出的词另受最终输出计数约束 |
| `MaxDepth` | 32 | TTML XML 嵌套层数，根为第 1 层 |
| `MaxDiagnostics` | 100 | 警告条数；超出即失败，不丢弃后续诊断继续解析 |

各字段零值选择默认值，负数是配置错误，正数显式覆盖。`Strict` 将第一条警告变为错误。失败均不返回半成品文档；此前已有的警告可随 `Result.Diagnostics` 返回。`ParseError` 提供错误代码及一基物理行/UTF-8 字节列，取消错误保留 `errors.Is(err, context.Canceled/DeadlineExceeded)` 语义。

`MaxTextBytes` 是展开文本的逻辑字节预算，不是峰值 heap/RSS 上限。XML 段落节点、解析 builder、行/词切片、诊断、最终快照和生成 ID 仍有结构及临时开销，分别由输入、条目和深度预算约束；解析完成后释放工作区引用，没有常驻解析缓存。

仅接受 UTF-8，可有 BOM，支持 LF/CRLF/CR；其他编码应由宿主先显式转换。非法编码不会被替换成问号继续。空文件返回合法空文档。输入扫描、逐行/逐词/XML token 转换和规范化期间检查上下文；排序及最终文档快照没有抢占式中断，取消延迟受到本次输入预算约束。调用期间不得改写输入；返回后可复用源字节。文档复制行、词和元数据，解析器对输出字符串解除源缓冲引用。

共同未知行尾使用下一**不同**开始时间、宿主 `Options.Duration`（绝对曲终，不随 offset 平移）或 `FinalLineDuration`（默认 **5 秒**，产生 `inferred-end`）。LRC 另按 AMLL 的结尾标记及末行 sentinel 处理；YRC/QRC 时长显式。源时间兼容模式保留零时长词和越界词；严格默认构造器仍拒绝它们。`Strict` 仍将每条诊断变为错误，不自动修复历史源时序。

## 各格式支持子集

| 格式 | 支持与时间单位 | 结束、偏移与异常 |
| --- | --- | --- |
| LRC | AMLL SPL/LRC 时间标签：1–3 位分、1–2 位秒、可选 1–6 位小数（点或冒号分隔，小数截至毫秒）；多个前置时间复制普通行 | offset 仅保留 metadata；键转小写、重复值去重；相同开始的后续文本与邻接未计时文本成为翻译；括号包围的行成为背景；最后普通行默认 `999:59.999` |
| 增强 LRC / ESLRC | 方括号或尖括号绝对词时间；末尾标签显式关闭行；保留源顺序与零时间空格 | 多个前置标签取第一个为行开始、最后一个为首词开始；末行缺尾时默认最后词开始后 1 秒；未知行尾使用下一排序行，包含同开始行；宿主 Duration 可覆盖推断末尾 |
| YRC | `[行开始,行时长](词开始,词时长,0)词文`，整数毫秒绝对时间；按 AMLL 惰性扫描标记 | 仅 flags=0 是时间 token，其他括号为标记间正文；空 payload 跳过，无有效标记的非空 payload 保留空词行；显式时长、metadata offset 平移；保留 `(0,0,0)` 空格 |
| QRC | `[行开始,行时长]词文(词开始,词时长)`，整数毫秒绝对时间；按 AMLL 惰性扫描标记 | 未匹配尾文忽略，无有效标记的非空 payload 保留空词行；显式时长、metadata offset 平移；保留 `(0,0)` 空格；不解包服务容器、加密或压缩 |
| TTML | W3C、legacy 或无命名空间的歌词 XML；p/span 绝对时间、iTunes 行键、背景、翻译/音译、timed Ruby、obscene/empty-beat | body/div 时间不作偏移；普通时钟与裸秒按毫秒取整，显式 Apple Word 小数秒及 h/m/ms 后缀是本库精度扩展；词/背景可扩展行范围；无隐式 offset |

YRC/QRC metadata 键以 ASCII 字母开头，其余可为字母、数字、下划线和连字符，统一转小写，值与重复顺序保留；LRC 键仅接收 ASCII 字母并转小写。三类明文格式都支持完整 ASCII/全角括号对的背景约定，YRC/QRC 不推断翻译/音译/对唱。TTML 的背景成为独立行并关联 ParentID，agent 类型与源顺序决定对唱侧。AMLL meta 值先裁剪外围 ECMAScript 空白、略过空值；ttm:title/name、根语言、timingMode、songwriters 及纯 audio 时间属性保留为元数据。普通 `xml:space` 不改变 AMLL 的空白规则。

TTML 支持当前及旧 iTunes namespace、继承 TTML namespace 的 iTunesMetadata、首语言 inline/sidecar 翻译/音译与逐词音译匹配；未选语言不构成无损保存承诺。seq、frame/tick、嵌套 timed span、词内 annotation、嵌套背景和一般样式布局仍不在本 profile 内。DTD、外部实体声明、位置不合法的 XML 声明和外部资源引用被拒绝；未声明前缀、重复展开属性与 QName 配对错误返回结构性诊断。无资源 audio 属性不会启动 I/O。具体边界见 [RFC 0005](rfcs/0005-m2-corpus-compatibility.md)。

M3 为已验收的实际 TTML 文件呈现增加受限 **Apple Word profile**：仅根 `tt` 上精确的 `http://music.apple.com/lyric-ttml-internal` 命名空间属性 `timing="Word"` 启用，可用任何前缀拼写；该 profile 接受无单位整数/小数秒。`body/div` 的 begin/end/dur 仍忽略，不叠加到 p/span 或背景词；p/span 的 `dur` 仍需显式 begin。此扩展不改变 M2 corpus compatibility 的服务范围，也不构成完整 Apple Music TTML 支持。

## 验证记录

测试使用合成 fixture；源码是本仓实现，未复制上游代码、歌词或媒体。覆盖正常与错误输入、BOM/乱码/换行、重复时间与身份、零时长词、背景/对唱、空文件、最后一行、大输入中途取消、所有预算、严格模式与缓冲所有权。视觉状态包含 6,999/7,000/7,001 ms 边界、前奏、长重叠、全局曲终、随机线性对照、双向 seek、并发和零分配契约。

本机 Windows/amd64、Go 1.27.3、Python 3.13.16、GCC race 工具链：

- `python scripts/check.py`：最终代码通过 gofmt、vet、全包 race 测试、Python 标题检查器测试与 tracked diff 空白检查，包含 M1 现有测试。
- 四种 `examples/formats` 合成输入在 `-strict` 下运行通过，分别验证活动、间奏、换行和曲终；原 `examples/timeline` 输出保持两条同时活动歌词。
- `go list -deps` 确认 `lyric` / `timeline` / `format/...` 的非标准库依赖只有本库包，不链接 mygo、音频或外部服务。
- 文档本地链接检查通过。没有新增窗口/GPU/RSS 实测，本地验收时尚未运行远程 CI；提交后的结果以对应 PR 检查为准。

### 时间查询基准

CPU：11th Gen Intel Core i5-1135G7 @ 2.40 GHz，基准后缀 `-8`。基线从提交 `df6ac1a7d586840281c908fb2c7301b0566ed3a0` 提取原 `lyric` / `timeline` Go 文件，在仓库外标准库独立模块测量；最终列为验收时的工作区。两次测量顺序运行，没有同时启动其他检查；没有锁定 CPU 频率/电源模式，因此这些是本机参考，不是统计显著的优化或回归结论。

每项重复 5 次，表中时间为 ns/op 的中位数。所有查询组均为 **0 B/op、0 allocs/op**，复用预分配的输出缓冲。

| 行数 / 长重叠 | 原 `ActiveAt` | 最终 `ActiveAt` | 新 `VisualAt` | `VisualAt` 5 次范围 |
| --- | ---: | ---: | ---: | ---: |
| 100 / 否 | 14.87 | 16.14 | 40.77 | 40.25–41.36 |
| 100 / 是 | 46.26 | 51.13 | 131.6 | 128.5–133.2 |
| 1,000 / 否 | 41.41 | 41.70 | 138.2 | 137.3–141.2 |
| 1,000 / 是 | 371.6 | 374.3 | 795.2 | 780.2–866.9 |
| 10,000 / 否 | 59.24 | 58.51 | 165.0 | 163.9–170.1 |
| 10,000 / 是 | 4,153 | 4,505 | 9,075 | 8,699–9,425 |

`ActiveAt` 算法未更改，但行模型增加元数据字段，可能改变缓存局部性；部分组的中位数有所上升，原始结果保留供复核。新增视觉查询包含两个时刻的区间查询及间奏/曲终计算，不能按只查活动行的成本要求它。长重叠仍体现候选扫描最坏 `O(n)`，没有通过额外每行索引存储换取查询结果。

### 解析冷路径基准

最终命令：`go test ./lyric ./timeline ./format/... ./internal/... -run '^$' -bench . -benchmem -count 5`。fixture 在计时外生成，每次解析建立完整文档；包含 UTF-8 校验、预算、规范化与快照，不含宿主 I/O。

| 合成输入 | 耗时中位数（ms/op） | 5 次范围（ms/op） | B/op 中位数 | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| 增强 LRC，200 行 | 0.164 | 0.158–0.204 | 216,539 | 1,817 |
| 增强 LRC，2,000 行 | 1.951 | 1.904–2.029 | 2,580,088 | 19,777 |
| TTML，200 行 | 0.735 | 0.703–0.825 | 431,461 | 8,437 |
| YRC，1,000 行 | 0.705 | 0.685–0.714 | 1,109,645 | 7,923 |
| QRC，1,000 行 | 0.654 | 0.608–0.708 | 1,109,644 | 7,923 |

原始基线与最终 17 组、每组 5 次输出保存在本机仓库外的临时文件，不进入版本控制：

```text
%TEMP%/mygo-amll-m2-before-20261109.txt
%TEMP%/mygo-amll-m2-final-20261109.txt
```

查询和解析都是纯 Go 基准，`B/op` 为每次操作累计分配，不是 live heap 或峰值内存；五次基准均值的范围不是逐帧 p95/p99，也不能证明原生帧率或整进程内存达标。
