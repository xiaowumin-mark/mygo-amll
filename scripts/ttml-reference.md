# 固定语料与 AMLL 原解析器差分

这些工具用于离线开发验证；纯 Go 核心、普通测试与 Native 示例无需 Node 或 npm。
不提交语料、生成的 JSONL 或参考构建产物。`-canonical` 和字段差异文件包含真实歌词，
应保留在本机临时目录。

固定来源：

- 数据库：`amll-dev/amll-ttml-db@6007fcdf33bf838bea0304292992bbdc1d6e2bea`。
- 行为参考：`amll-dev/applemusic-like-lyrics@f32285864056587c4afa59d310208b0aa829cb53`。
- 参考入口：未修改的 `packages/ttml/src/index.ts` 中 `TTMLParser` 和 `toAmllLyrics`。
- 工具依赖：Node 22+、`esbuild@0.25.12`、`@xmldom/xmldom@0.9.10`。
  两个 npm 包只安装到显式临时目录，不加入 Go 模块或项目依赖。

先准备两个干净的参考 checkout，并检出以上完整 commit。工具拒绝参考仓库的 tracked
修改和 commit 不符。若确实要验证其他快照，应显式传入 `--expect-corpus` / `--expect-amll`
并单独报告结果，不将其混入固定快照的证据。

下面以两个参考仓库位于当前 Go checkout 的同级目录为例，在 Go checkout 根执行：

```powershell
$referenceDeps = Join-Path $env:TEMP 'mygo-amll-ttml-reference-deps'
npm install --prefix $referenceDeps --no-save --package-lock=false esbuild@0.25.12 @xmldom/xmldom@0.9.10
$corpusRecords = Join-Path $env:TEMP 'mygo-amll-corpus-canonical.jsonl'
$referenceReport = Join-Path $env:TEMP 'mygo-amll-reference-report.json'
$referenceDifferences = Join-Path $env:TEMP 'mygo-amll-reference-differences.jsonl'
go run ./scripts/ttml-corpus -corpus ../amll-ttml-db -expect-corpus 6007fcdf33bf838bea0304292992bbdc1d6e2bea -canonical -records $corpusRecords
node scripts/ttml_reference.mjs --corpus ../amll-ttml-db --amll ../applemusic-like-lyrics --deps $referenceDeps --go-records $corpusRecords --report $referenceReport --differences $referenceDifferences
```

第一步从 Git index 发现全部 tracked TTML 路径，使用默认解析预算，逐文件检查两次解析
确定性、输入字节所有权、ID/背景关联和当前模型的时间不变量。报告记录 corpus/parser
commit、parser 工作区是否有未提交修改，以及异常源时间计数。词与 Ruby 的原始反向、
零时长、越界和非单调时间保留并计数，不为通过检查而交换、排序或丢弃字段。

第二步把固定 AMLL 的 TypeScript 参考在系统临时目录编译为 Node ESM，读取同一检出
文件的字节。它按 Git blob 对重复平台路径去重，但仍核验每条 Go 记录、全部路径覆盖
和同 blob 输出一致性。参考输出和 Go 输出逐字段比较，任何未列出的差异均失败。
输出摘要记录依赖版本、两个 commit 和规范化结果集合 hash；字段差异 JSONL 不截断，提供路径、blob、JSON
字段位置、期望值和实际值。没有 `--differences` 时，差异写到标准错误。

共同模型的规范化规则固定为：

- 行按 `startMS` 稳定排序；相同开始时间的文本顺序保留。词和 Ruby 保持原顺序。
- 比较全部逐词文本、逐词音译、Ruby、obscene、empty-beat、翻译、行音译、时间端点、
  背景与对唱字段。时间精确比较；不加容差，不 trim 字符串，不做 Unicode 归一化。
- metadata 仅忽略 key 的顺序、去重同 key 下重复值，并移除 AMLL 默认生成的空值数组。
  值的顺序保留，空字符串值不丢弃；`title` 映射为共同字段 `musicName`。
- Go 明确保留的额外 metadata `name`（agent display name）、`lyricOffset`、`audioRole`
  不进入共同 metadata。任何其他额外非空 key 均视为差异。
- Go 的 ID、ParentID、完整行 Text、Agent、Language 是简化 AMLL 输出之外的字段，仍在
  Go 工具的完整文档 snapshot hash / 不变量检查中保留，但不参与共同模型差分。

共同模型与 AMLL 的 `toAmllLyrics` 一样，只选择首个翻译/音译语言。本验证不等于无损
TTML 往返兼容，也不替代 Native 渲染、字体、GPU 或视觉验收。原参考本身解析失败的
输入默认导致验证失败。唯一通用例外是双方确实拒绝结构错误的 XML：Go 必须提供
`xml` 或 `namespace` 诊断且没有半成品文档、字段、snapshot；参考错误必须实际发生在
DOMParser 的 XML/命名空间阶段。这些输入单独计为 `rejectedInvalidBlobs/Paths`，与
成功匹配的有效输入分开，不作为有效歌词成功解析数。Go `ttml` profile、time、limit
等错误，或只一方失败，仍使验收失败。工具不按文件添加白名单。第一步的 Go 命令会
对解析错误退出非零；最终是否为正确拒绝，由第二步的双边证据判定。

合成差分规则回归测试仅需要 Node 标准库：

```powershell
node --test scripts/ttml_reference.test.mjs
```
