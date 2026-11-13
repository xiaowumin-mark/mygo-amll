# 第三方来源与许可

本库的项目许可为 **AGPL-3.0-only**。此文件记录调研与后续移植边界，不替代各许可证正文。

| 项目 | 固定调研 commit | 许可 | 当前用途 |
| --- | --- | --- | --- |
| [AMLL](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53) | `f32285864056587c4afa59d310208b0aa829cb53` | AGPL-3.0-only；spring 另有 MIT 来源标记 | M2 格式与 M3 布局、显示规范化、动画/遮罩的 Go 适配来源；本地 `ref/` 不跟踪/分发 |
| [AMLL TTML DB](https://github.com/amll-dev/amll-ttml-db/tree/6007fcdf33bf838bea0304292992bbdc1d6e2bea) | `6007fcdf33bf838bea0304292992bbdc1d6e2bea` | 自主编写部分 CC0-1.0；外来数据依原提供方授权 | 所有者指定的本地解析兼容性语料；歌词、仓库和生成输出不随库分发 |
| [mygo](https://github.com/egoist/mygo/tree/eadf1fce6af3bdb9f7cacf649823de2bee2737f2) | `eadf1fce6af3bdb9f7cacf649823de2bee2737f2` | MIT | M1 原生示例的固定依赖；核心包 import 图仍无 UI |

当前 `lyric`、`timeline`、`format` 解析器和示例是本仓新建的 Go 实现，并非已经完成的 AMLL 渲染器移植。新增测试和示例文本均为合成输入。本轮 M2 格式兼容算法在阅读并执行下列固定源码后按其行为重新实现，具体映射见下表；不能把这部分算法来源写成与上游完全无关。添加实际复制/翻译的代码、shader、fixture 或图片时，必须逐项记录原作者、源文件、commit、许可证与修改，并保留必要版权声明。mygo 成为实际依赖时须遵守其 MIT notice；构建的分发物应包含所需第三方声明。

| M2 参考来源（AMLL contributors） | 本仓实现与改动 |
| --- | --- |
| [`packages/ttml/src/parser.ts`](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/ttml/src/parser.ts)，固定提交 `f32285864056587c4afa59d310208b0aa829cb53`，AGPL-3.0-only | `format/ttml` 的绝对时间、空白、背景区间、Ruby、内联注释和 Apple sidecar 结构规则；使用 Go XML 流和受限段落/sidecar 工作区重新实现，增加命名空间严格验证、资源预算、取消、诊断及不可变文档快照 |
| [`packages/ttml/src/utils/amll-converter.ts`](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/ttml/src/utils/amll-converter.ts)，同一固定提交，AGPL-3.0-only | `format/ttml/agents.go` 的 typed agent 对唱交替、`sidecar.go` 的首注释选择及逐词音译匹配；以 Go 重写，增加每次解析的有界匹配工作量和取消检查，保留宿主需要的稳定行身份/背景关联 |
| [`packages/lyric/src/formats/lrc/index.ts`](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/lyric/src/formats/lrc/index.ts)、同目录 [`parser.ts`](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/lyric/src/formats/lrc/parser.ts)、`helpers.ts` / `metadata.ts`，同一固定提交，AGPL-3.0-only | `format/lrc` 的普通/增强 LRC 时间标签、逐词文本、背景括号、辅助行、metadata 去重与行词结束推断；以 Go 重写，保留源时间并增加输入/输出预算、取消检查和不可变快照，未引入上游 generator 或运行依赖 |
| [`packages/lyric/src/formats/yrc.ts`](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/lyric/src/formats/yrc.ts) 与 [`qrc.ts`](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/lyric/src/formats/qrc.ts)，同一固定提交，AGPL-3.0-only | `format/yrc`、`format/qrc` 及 `internal/timedtext` 的明文行头、词标记惰性扫描、空格、背景括号和原始端点行为；以 Go 扫描器重新实现，增加数字溢出保护、资源预算、取消和稳定行排序，不包含服务容器、压缩或加密格式 |

以上 TypeScript 源文件和其运行依赖未直接作为本库模块或生成产物加入。算法修改在本库 AGPL-3.0-only 下提供，来源说明不替代上游及本库的许可证正文。具体数据契约与兼容扩展见 [RFC 0005](docs/rfcs/0005-m2-corpus-compatibility.md)。

AMLL TTML DB 的 [共享说明](https://github.com/amll-dev/amll-ttml-db/blob/6007fcdf33bf838bea0304292992bbdc1d6e2bea/README.md#共享协议) 明确区分提交者自主编写和外来数据，不能把根 CC0 文件解释为所有歌词均可任意再分发。本次仅按所有者要求在被忽略的 `ref/` 中进行兼容性验证；不复制歌词样本到测试、不分发语料或解析后的歌词输出。回归测试使用等价的合成结构。

M3 参考渲染已适配固定 AMLL 快照。`examples/native/word_layout.go` 的词重组/平衡换行参考 `utils/{lyric-split-words,lyric-line-break,line-balancer,is-cjk}.ts` 与 `lyric-player/{base/line,dom/lyric-line}.ts`；`presentation.go` 对应 `utils/optimize-lyric.ts`；效果及状态管线对应 `lyric-player/dom/animation/{mask,float,emphasize}`、`line-brightness.ts`、`base/group.ts` 等。实现保留源路径、commit 和 Native/Go 适配说明，适用 AGPL-3.0-only。Native atlas、Kage shader 和资源预算为本仓实现，没有复制 WebGL 背景 shader。原仓未带单独作者版权头的文件不伪造版权行。

`internal/motion` 的弹簧另保留源文件 `/** MIT License github.com/pushkine/ */` 标记及原始来源；Bezier 行为参考 `bezier-easing@3.0.1`（MIT，Copyright (c) 2014 Gaëtan Renaudeau）。来源、原始声明、适用许可和修改见 [motion NOTICE](internal/motion/NOTICE.md)。Node 仅用于可选参考测试，不是本库运行依赖。

Windows CJK 字体回退参考 Chromium [`1aba5922274bbff7e4a15411c48daa980e2cb31b`](https://github.com/chromium/chromium/blob/1aba5922274bbff7e4a15411c48daa980e2cb31b/third_party/blink/renderer/platform/fonts/win/font_fallback_win.cc) 的脚本/区域字体优先表，将其适配为公开 Native 字体 family 选择和有界决策缓存，不引入 Chromium 运行时或字体文件。该来源为 BSD-3-Clause，完整原始文件头保存在 [字体回退许可](LICENSES/chromium-font-fallback.txt)。它针对固定 Chrome 实测环境，不代表移植了全部浏览器字体匹配。

`examples/native/emoji_shape_windows.go` 的 emoji 分类还参考同一 Chromium 提交的 emoji segmentation header 和字体 fallback 顺序（Copyright 2019 The Chromium Authors；BSD-3-Clause，见 [Chromium 声明](LICENSES/chromium-emoji-segmentation.txt)），以及 [google/emoji-segmenter 0.3.0](https://github.com/google/emoji-segmenter/blob/955936be8b391e00835257059607d7c5b72ce744/emoji_presentation_scanner.rl) 固定提交 `955936be8b391e00835257059607d7c5b72ce744`（Copyright 2019 Google LLC；Apache-2.0，见 [许可全文](LICENSES/emoji-segmenter.txt)）。本库使用 ICU 属性及字体覆盖实现 Go 分类，未复制 Ragel 生成的 C scanner，也不引入其运行时。

M1 使用 mygo 的公开 API，没有复制其绘制后端或 AMLL 源码。实际使用的第三方模块及许可原文如下；`LICENSES/` 只保留对应固定模块的 LICENSE，未修改正文。发布可执行分发物时须随附所需许可/notice，并重新检查其完整依赖与文件级声明。

| 模块 | 锁定版本 | 许可与原文 |
| --- | --- | --- |
| `github.com/egoist/mygo` | `v0.8.4-0.20261109104234-eadf1fce6af3` | [MIT](LICENSES/mygo.txt)，Copyright (c) 2026 MyGo contributors；M1/M3 公开 Native API |
| `github.com/ebitengine/purego` | `v0.11.1` | [Apache-2.0](LICENSES/purego.txt) |
| `github.com/go-text/typesetting` | `v0.3.5` | [Unlicense OR BSD-3-Clause](LICENSES/typesetting.txt) |
| `golang.org/x/image` | `v0.46.0` | [BSD-3-Clause](LICENSES/x-image.txt)；截图使用的 Go 字体另有字体专用声明 |
| `golang.org/x/net` | `v0.59.0` | [BSD-3-Clause](LICENSES/x-net.txt)，本示例 Windows 依赖 publicsuffix |
| `golang.org/x/text` | `v0.43.0` | [BSD-3-Clause](LICENSES/x-text.txt) |

上游 module 图中的音频等其他依赖不等于本示例实际链接的 package。运行示例默认使用系统字体与上游回退；字体安装/再分发授权由宿主另行确认。M3 截图测试通过公开 `RegisterFont` 使用 `golang.org/x/image/font/gofont/goregular` 与 `gobold`。其字体专用声明为 Copyright (c) 2016 Bigelow & Holmes Inc.，BSD-3-Clause；[Go 字体许可原文](LICENSES/go-fonts.txt)不会以模块通用版权代替字体版权，也未复制字体二进制到仓库。它们固定 Latin 测试字体，非 Latin 字形仍来自系统 fallback。

AMLL 的部分背景 shader 还包含进一步来源，例如 `packages/core/src/bg-render/isolation/isolation.frag.glsl` 声明来自 Storyteller-Studios/Cirrus。不能仅凭仓库根许可就忽略文件级来源；背景实施前逐项核对，不在本次初始化中搬入这些 shader。

本项目不会把未授权歌曲、歌词、封面或上游测试媒体作为默认资产。测试与示例使用合成或明确授权的内容。Apple Music 名称仅用于描述参考交互；本项目没有声明与 Apple 存在隶属关系。

将来若希望采用不同许可，必须先确认所有贡献与上游材料的授权范围，不能通过修改根 LICENSE 将已有 AGPL 材料重新标记为宽松许可。作为通用库集成不意味着自动免除 AGPL 的适用义务。
