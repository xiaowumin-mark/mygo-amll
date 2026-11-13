# AMLL 解析器对照

状态：2026-11-10。基准是本地只读快照 `ref/applemusic-like-lyrics`，commit
`f32285864056587c4afa59d310208b0aa829cb53`。本表逐项比较 AMLL 的 LRC 家族、YRC、QRC、TTML 解析行为与本库输出，不代表两个项目的 API 或内部数据结构相同。固定上游位置见各项引用；本地位置以解析器包为准。

## 已对齐的输入行为

| 格式 | 对照结果 |
| --- | --- |
| LRC / SPL | 空行和行首注释、metadata 小写归一与重复值合并、多个前置时间标签去重展开、增强词时间标签、时间戳单独成行时关闭前组末行、无时间翻译附加到前组、带时间翻译按开始时间寻找先前主行、全行圆括号背景标记、稳定按开始时间排序、普通末行哨兵与最后词一秒收尾。畸形括号中的后续合法时间标签现会继续扫描。固定参考：`packages/lyric/src/formats/lrc/parser.ts`、`formats/lrc/metadata.ts`、`formats/lrc/helpers.ts`。 |
| YRC | 行级毫秒区间、完整 `(start,duration,0)` 词标记、惰性匹配间任意括号/文本作为词文本、非零 flags 不匹配为普通文本、整行首尾括号背景标记、行和 payload 外围空白裁剪、空 payload 跳过。固定参考：`packages/lyric/src/formats/yrc.ts`。 |
| QRC | 行级毫秒区间、惰性 `(start,duration)` 词标记、普通括号文本到后续有效标记前作为词文本、无有效词标记时保留空词行、未匹配尾部忽略、背景括号剥除、外围空白裁剪和空 payload 跳过。固定参考：`packages/lyric/src/formats/qrc.ts`。 |
| TTML | 常见 `tt/head/metadata/body/div/p/span/br` 歌词树、逐词与背景/翻译/音译 inline role、`itunes:key`、绝对词/行时间、格式空白节点规则、`<br>` 不添加换行、Apple 毫秒取整与时间属性本地名回退；`body/div` 时间被忽略，p 的范围扩展到包含定时词和背景；没有逐词标记的有效正文段落会生成覆盖整行时间的单词，只有完整 begin/end 的 span 才生成定时词。固定参考：`packages/ttml/src/parser.ts` 与 `packages/ttml/src/constants.ts`。 |

LRC 的翻译语义在本库映射到 `Line.Translation`，没有像 AMLL 一样在返回行列表中展开为独立翻译行；这是目标数据模型的表达方式差别。YRC/QRC/TTML 的词文本与绝对时间仍按共同的只读歌词模型输出。

## 已知的不等价范围

“确保逻辑一样”不能对所有输入成立，以下差异须在范围或模型改变前保留为明确限制：

| 差异 | AMLL 行为 | 本库行为与原因 |
| --- | --- | --- |
| LRC 逆序词时间 | 接受并保留源次序，可能形成逆序或零时长区间；参考 `lrc/parser.ts` 的 `parseWordSyncLine`。 | 报错。核心文档要求词开始时间非递减，并要求正时长区间。 |
| 零时长行/词 | 可保留解析结果中的零时长数据。 | 规范化时移除零时长词并保留行文本；零/负时长行报错。核心区间为 `[start,end)` 且必须 `start < end`。 |
| 行输入次序 | 多数解析器按源次序返回；LRC 最终按开始时间排序。 | 共享 `formatutil.Finish` 对所有格式稳定按开始时间排序；YRC/QRC/TTML 的未排序源输入会不同。 |
| 最终相同开始时间的多行 | AMLL 的 LRC 末尾推导只把最终排序行视为最后一行。 | 最后一条相同最大开始时间行使用 AMLL 末行规则；前面的同起点行保留安全的本地默认时长，避免生成零长度区间。 |
| TTML 元数据扩展 | 保留 root 语言/时间模式、typed agents、iTunes 逐词翻译/音译 sidecar、song-part 与 block 索引等；参考 `ttml/src/parser.ts`。 | `iTunesMetadata` sidecar 现在整体跳过并发出一条诊断，主歌词可以继续解析，但逐词转写/翻译内容未保留。完整 sidecar、song-part/block 数据需要单独的公共模型/API 设计。 |
| TTML 行身份 | Apple 行依赖 `itunes:key`，没有 key 的段落可被跳过。 | 优先使用 `itunes:key`，为既有调用兼容 `xml:id` 并在两者缺失时生成稳定的文档局部 ID。 |
| TTML 时间/错误 | 上游将部分缺失或无效值转成 0，并支持浏览器 DOM 可表达的宽泛结构。 | 拒绝无效时间、资源 URI、DTD、外部实体和超出支持 profile 的结构；采用正时长绝对时间模型及输入/深度/输出预算。 |
| 数值与输入预算 | JavaScript Number 与字符串处理不提供本库的相同边界。 | 本库校验 UTF-8、整数/Duration 溢出、输入和展开输出上限，并支持取消；不会为兼容而去掉这些约束。 |

这些是实现差异，不可描述为“逐项完全等价”。尤其要区分合法常见输入的字段映射一致性与异常、零区间、扩展元数据、输入顺序的全量行为一致性。

## 回归验证

定向命令：

```sh
go test ./format/lrc ./format/yrc ./format/qrc ./format/ttml ./internal/timedtext -count=1
```

覆盖新增的解析修复：LRC 畸形标记后的扫描恢复及相同最大起始时间末行推导；YRC lazy flags-0 匹配、括号文本、空白与空 payload；QRC 普通括号、无效尾部及 malformed-only 空词行；TTML 命名空间属性回退、无定时 span 的普通行和不完整 span。该测试验证 Go 行为，不替代上述核心模型差异，也不扩大各格式支持子集。
