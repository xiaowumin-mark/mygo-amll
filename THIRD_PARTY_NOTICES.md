# 第三方来源与许可

本库的项目许可为 **AGPL-3.0-only**。此文件记录调研与后续移植边界，不替代各许可证正文。

| 项目 | 固定调研 commit | 许可 | 当前用途 |
| --- | --- | --- | --- |
| [AMLL](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53) | `f32285864056587c4afa59d310208b0aa829cb53` | AGPL-3.0-only | 行为、模块与性能设计参考；本地 `ref/` 不跟踪/分发 |
| [mygo](https://github.com/egoist/mygo/tree/eadf1fce6af3bdb9f7cacf649823de2bee2737f2) | `eadf1fce6af3bdb9f7cacf649823de2bee2737f2` | MIT | Native UI 目标后端的公开 API 调研；当前 Go 核心尚未依赖 |

当前 `lyric`、`timeline` 和示例是本仓新建的最小 Go 实现，并非已经完成的 AMLL 渲染器移植。添加实际复制/翻译的代码、shader、fixture 或图片时，必须逐项记录原作者、源文件、commit、许可证与修改，并保留必要版权声明。mygo 成为实际依赖时须遵守其 MIT notice；构建的分发物应包含所需第三方声明。

AMLL 的部分背景 shader 还包含进一步来源，例如 `packages/core/src/bg-render/isolation/isolation.frag.glsl` 声明来自 Storyteller-Studios/Cirrus。不能仅凭仓库根许可就忽略文件级来源；背景实施前逐项核对，不在本次初始化中搬入这些 shader。

本项目不会把未授权歌曲、歌词、封面或上游测试媒体作为默认资产。测试与示例使用合成或明确授权的内容。Apple Music 名称仅用于描述参考交互；本项目没有声明与 Apple 存在隶属关系。

将来若希望采用不同许可，必须先确认所有贡献与上游材料的授权范围，不能通过修改根 LICENSE 将已有 AGPL 材料重新标记为宽松许可。作为通用库集成不意味着自动免除 AGPL 的适用义务。
