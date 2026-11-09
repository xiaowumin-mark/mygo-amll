# 第三方来源与许可

本库的项目许可为 **AGPL-3.0-only**。此文件记录调研与后续移植边界，不替代各许可证正文。

| 项目 | 固定调研 commit | 许可 | 当前用途 |
| --- | --- | --- | --- |
| [AMLL](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53) | `f32285864056587c4afa59d310208b0aa829cb53` | AGPL-3.0-only | 行为、模块与性能设计参考；本地 `ref/` 不跟踪/分发 |
| [mygo](https://github.com/egoist/mygo/tree/eadf1fce6af3bdb9f7cacf649823de2bee2737f2) | `eadf1fce6af3bdb9f7cacf649823de2bee2737f2` | MIT | M1 原生示例的固定依赖；核心包 import 图仍无 UI |

当前 `lyric`、`timeline` 和示例是本仓新建的最小 Go 实现，并非已经完成的 AMLL 渲染器移植。添加实际复制/翻译的代码、shader、fixture 或图片时，必须逐项记录原作者、源文件、commit、许可证与修改，并保留必要版权声明。mygo 成为实际依赖时须遵守其 MIT notice；构建的分发物应包含所需第三方声明。

M1 使用 mygo 的公开 API，没有复制其绘制后端或 AMLL 源码。实际使用的第三方模块及许可原文如下；`LICENSES/` 只保留对应固定模块的 LICENSE，未修改正文。发布可执行分发物时须随附所需许可/notice，并重新检查其完整依赖与文件级声明。

| 模块 | 锁定版本 | 许可与原文 |
| --- | --- | --- |
| `github.com/egoist/mygo` | `v0.8.4-0.20261109104234-eadf1fce6af3` | [MIT](LICENSES/mygo.txt)，Copyright (c) 2026 MyGo contributors |
| `github.com/ebitengine/purego` | `v0.11.1` | [Apache-2.0](LICENSES/purego.txt) |
| `github.com/go-text/typesetting` | `v0.3.5` | [Unlicense OR BSD-3-Clause](LICENSES/typesetting.txt) |
| `golang.org/x/image` | `v0.46.0` | [BSD-3-Clause](LICENSES/x-image.txt) |
| `golang.org/x/net` | `v0.59.0` | [BSD-3-Clause](LICENSES/x-net.txt)，本示例 Windows 依赖 publicsuffix |
| `golang.org/x/text` | `v0.43.0` | [BSD-3-Clause](LICENSES/x-text.txt) |

上游 module 图中的音频等其他依赖不等于本示例实际链接的 package。本例没有加载外部字体文件，使用系统字体与上游回退；字体安装/再分发授权由宿主另行确认。

AMLL 的部分背景 shader 还包含进一步来源，例如 `packages/core/src/bg-render/isolation/isolation.frag.glsl` 声明来自 Storyteller-Studios/Cirrus。不能仅凭仓库根许可就忽略文件级来源；背景实施前逐项核对，不在本次初始化中搬入这些 shader。

本项目不会把未授权歌曲、歌词、封面或上游测试媒体作为默认资产。测试与示例使用合成或明确授权的内容。Apple Music 名称仅用于描述参考交互；本项目没有声明与 Apple 存在隶属关系。

将来若希望采用不同许可，必须先确认所有贡献与上游材料的授权范围，不能通过修改根 LICENSE 将已有 AGPL 材料重新标记为宽松许可。作为通用库集成不意味着自动免除 AGPL 的适用义务。
