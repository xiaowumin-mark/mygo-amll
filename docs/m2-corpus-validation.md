# M2 全库解析验证

日期：2026-11-11。本轮由所有者要求验证完整 AMLL TTML 数据库，再仅提交、合并 M2 修改。实现与验证在从 `24d07407585941422bc99cb5f267a21f24e86a08` 创建的独立 worktree 完成；主工作区的 Native/M3 文件和暂存区未参与本次提交。没有发布版本。

## 固定输入与判定标准

| 来源 | 固定提交 |
| --- | --- |
| `amll-dev/amll-ttml-db` | `6007fcdf33bf838bea0304292992bbdc1d6e2bea` |
| `amll-dev/applemusic-like-lyrics` | `f32285864056587c4afa59d310208b0aa829cb53` |

数据库保存在被忽略的 `ref/amll-ttml-db`，Git tracked 内容未改动。TTML 共 15,959 路径、3,452 个 Git blob、528,074,988 检出字节，最大文件 212,792 字节。包括 `raw-lyrics`、四个平台目录和 `Issue-files`，不只验证最新平台条目；516 个路径的 CRLF 检出变换经 LF blob 精确比对证明，解析仍使用原始检出字节。

Go 全路径验证使用默认 `format.Options`，没有放大预算或按文件放行。每个 TTML 解析两次，比较完整快照与诊断；改写输入缓冲后再次检查文档不变，同时验证身份、ParentID 和源时间契约。worker 默认上限为四，同一批完成后释放文档；不同时驻留完整语料。

行为参考直接执行固定、未修改的 `TTMLParser + toAmllLyrics` 与原 LRC/YRC/QRC 解析入口。参考的 TypeScript 仅在临时目录编译为 Node ESM，不进入本库运行依赖。比较正文词、逐词音译、Ruby、obscene、empty-beat、翻译、行音译、时间端点、背景、对唱和共同 metadata；字符串和毫秒数精确相等，不裁剪比较值、不归一化 Unicode、不交换时间、不做文件白名单。

模型差异只按固定规则处理：行按开始时间稳定排序，词/Ruby 保持源顺序；metadata 忽略 key 顺序并去重同值，`title` 映射 `musicName`，忽略参考默认生成的空数组。Go 的 `name`、`lyricOffset`、`audioRole` 是额外保留的 metadata。ID/ParentID/完整行 Text/Agent/Language 在完整 Go 快照中检查。简化模型选择首语言，与默认 `toAmllLyrics` 一致，不代表无损 TTML 往返或保存所有 sidecar 语言/逐词翻译数组。

## 全量结果

| 格式 | 路径 | 按格式去重内容 | 成功解析 / 精确匹配 | 未解决字段差异 |
| --- | ---: | ---: | ---: | ---: |
| TTML | 15,959 | 3,452 | 15,953 路径 / 3,451 内容 | 0 |
| LRC | 10,897 | 2,584 | 全部 | 0 |
| ESLRC（增强 LRC） | 10,897 | 2,590 | 全部 | 0 |
| YRC | 10,897 | 2,591 | 全部 | 0 |
| QRC | 10,897 | 2,591 | 全部 | 0 |

共验证 59,547 个已支持格式路径。数据库另有 `.lys` 派生文件；Lyricify Syllable 不属于现有 M2 格式接口，本轮没有新增或宣称验证该解析器。

有效 TTML 输出共 811,279 行、6,559,307 个时间词、5,962 个 Ruby 段。参考与 Go 的规范化有效全集 SHA-256 都是：

```text
3681ed7605e49ab905e7b03cdd897c44840780f2b621494f61d0dbf150b51961
```

其他生成格式分别输出 LRC 547,715 词、ESLRC 5,735,220 词、YRC 5,745,561 词、QRC 5,748,070 词。LRC 的 Translation 按固定模型规则展开为参考的独立注释行再比较；所有词文本和时间均保留。没有通过删除零时长空格、重排时间词或修改源文件取得一致。

四种生成格式的规范化全集 SHA-256 同为 `ca5669e3f268b56010eac85a9e3a90cd44f7aef836e0338fb8e10af118efa24f`。可复现命令见 [生成格式验证工具](../scripts/format-reference.md)。

### 非法 XML 的明确拒绝

1 个唯一 TTML（6 个复本）缺少 `xmlns:amll` 声明，却使用 `amll:meta`。固定原解析器在 XML 阶段报 NamespaceError，Python ElementTree 也确认 unbound prefix。Go 现在返回可定位的 `namespace` 错误，无半成品文档；未自动补声明。原始路径是 `raw-lyrics/1793807241994-250306205-VJiQaCdN.ttml`，其余复本位于 am/ncm/qq/spotify 目录。

上述 6 个路径不是成功解析的歌词。差分工具仅在参考 XML 阶段拒绝、Go 的结构性诊断代码为 `xml/namespace` 且无文档/快照时报告 rejection parity；其他解析错误、预算错误、profile 拒绝或单方失败仍使验证失败。此规则适用于任意输入，不按上述文件身份排除。全路径解析命令因此会以非零退出并列出这 6 个源错误；随后参考差分会分别报告 `rejectedInvalidBlobs=1`、`rejectedInvalidPaths=6` 和有效字段零差异。

### 源时序异常与诊断

有效 TTML 原值包含 86 个 point 行、2,154 个 point 词、187 个反向词、452 处词开始时间倒退、288 个越行词；计数可以交叠。ESLRC 的 183 个路径含生成器末尾零标签形成的反向行。它们是作者/生成器时序事实，解析器保留原端点和文本，不能将兼容成功称为打轴准确。正常构造器仍严格；显式源时间选项和空活动区间行为见 [RFC 0005](rfcs/0005-m2-corpus-compatibility.md)。

TTML 全量诊断为 `inferred-end=132`、`reversed-word=17`、`sorted=580`、`unsupported-element=50`。反向词按文档诊断一次，因此 17 不等于 187；未支持的扩展仍可诊断，验证未隐去这些记录。`Strict` 会把诊断升级为错误；全语料兼容验证使用默认模式，不声称所有历史文件可通过 strict。

## 可复现工具与检查边界

完整命令、固定参考依赖和规范化规则见 [参考工具说明](../scripts/ttml-reference.md)。Go 命令为 `go run ./scripts/ttml-corpus`，逐字段参考命令为 `node scripts/ttml_reference.mjs`；真实歌词 JSONL、差异文件、参考构建和基准原始输出只留在本机 `ref/` 或系统临时目录，不提交。新增回归测试均为等价合成输入。

这些结果覆盖固定快照与所列接口，不能证明任意未来输入永无问题，也不替代 Native 渲染、字体、窗口/GPU/RSS 或所有平台运行验收。后续数据库或参考升级必须重新固定提交并运行全量检查。

本机 Go 1.27.3、Python 3.13.16、GCC 16.1.0、Node 22.19.0 下，`python scripts/check.py` 的 gofmt、vet、全包 race、3 项 Python 标题检查及 tracked whitespace 均通过；5 项 Node 差分规则测试和生成格式脚本语法检查通过。新增回归覆盖源端点/词序、point/反向行 seek、Unicode 词间空格、背景注释、首语言选择、typed agent、命名空间/重复属性/XML 声明、取消与预算。源文件未引入新的 Go 依赖，Native/M3 实现未纳入本次提交。

## 冷路径与查询基准

环境：Windows/amd64、Go 1.27.3、Intel i5-1135G7；基线为 `24d0740` 的独立干净 worktree，候选为本轮隔离工作区。两者顺序运行相同命令，每项 5 次、每次 100 ms；没有并行全库或 race 工作负载，未锁定 CPU 频率/电源模式。原始输出保留在 `%TEMP%/mygo-amll-m2-corpus-before.txt` 与 `mygo-amll-m2-corpus-after.txt`。

```sh
go test ./format/ttml ./timeline -run '^$' -bench 'BenchmarkParse|BenchmarkActiveAt|BenchmarkVisualAt' -benchmem -benchtime=100ms -count=5
```

TTML 合成 200 行冷解析中位数从 0.720 ms、431,464 B/op、8,437 allocs/op 变为 0.951 ms、450,848 B/op、8,444 allocs/op。新增 XML namespace 校验、源时间/背景关联、注释与 metadata 处理增加了冷路径成本；不将其包装成性能优化。所有 ActiveAt/VisualAt 查询规模与重叠组仍为 **0 B/op、0 allocs/op**；10,000 行长重叠 VisualAt 的中位数为基线 7.596 µs、候选 7.951 µs。短 CPU 样本不用于宣称统计显著回归，也不是 UI 帧间隔、峰值 heap/RSS 或 GPU 测量。
