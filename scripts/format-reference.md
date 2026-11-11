# 生成 LRC / ESLRC / YRC / QRC 全库验证

来源固定为 `amll-ttml-db@6007fcdf33bf838bea0304292992bbdc1d6e2bea` 和
`applemusic-like-lyrics@f32285864056587c4afa59d310208b0aa829cb53`。参考仓库必须
没有 tracked 修改。工具仅在系统临时目录编译未修改的 `packages/lyric/src/formats`
源文件；普通 Go 包与测试不需要 Node、npm 或参考仓库。

在隔离 Go checkout 根执行；下例参考仓库位于其同级目录：

```powershell
$formatDeps = Join-Path $env:TEMP 'mygo-amll-format-reference-deps'
npm install --prefix $formatDeps --no-save --package-lock=false esbuild@0.25.12
$formatRecords = Join-Path $env:TEMP 'mygo-amll-format-records.jsonl'
$formatReport = Join-Path $env:TEMP 'mygo-amll-format-report.json'
$formatDifferences = Join-Path $env:TEMP 'mygo-amll-format-differences.jsonl'
go run ./scripts/format-corpus -root ../amll-ttml-db -canonical -records $formatRecords
node scripts/format_reference.mjs --corpus ../amll-ttml-db --amll ../applemusic-like-lyrics --deps $formatDeps --go-records $formatRecords --report $formatReport --differences $formatDifferences
```

Go 命令从 Git index 发现全部四种格式路径，使用默认 `format.Options` 和有界 worker；
默认四个 worker，`-workers` 可设 1–32。输入按默认 8 MiB 上限读取，解析后释放文档，
相同源字节必须产生相同结果。报告列出 corpus/parser commit、parser 是否脏、逐格式
成功/失败数及诊断。失败退出非零；`-canonical` 只为每份唯一内容输出一次完整字段。

Node 命令检查全部路径覆盖、同 Git blob 一致性及 Go 记录对应的实际检出字节，逐份
唯一内容比较原文、行/词时间端点与背景标记。QRC/YRC 参考行按开始时间稳定排序，
匹配 Go 的时间索引模型；Go LRC 的 `Translation` 按原出现顺序展开为原解析器产生的
独立注释行。除这些明确模型规则外，不 trim 文本、不修复时间、不加容差或按文件
白名单。比较实际字段，并核对规范化 SHA256，任一字段、参考错误或证据缺失均失败。
这些生成格式本身不承载 TTML 的全部 Ruby、角色或多语言元数据；完整 TTML 差分见
[TTML 验证](ttml-reference.md)。LYS 不在当前 M2 的受支持格式范围。

记录和字段差异可能含真实歌词，应留在临时目录，不提交到仓库。脚本清理自己创建的
临时参考构建目录；`ref/` 中的参考源码不会被修改或提交。
