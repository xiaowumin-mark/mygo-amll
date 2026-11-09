# mygo-amll

使用 [mygo Native UI](https://github.com/egoist/mygo) 与自定义 GPU 绘制实现的可嵌入歌词库，行为目标参考 [Apple Music-like Lyrics](https://github.com/amll-dev/applemusic-like-lyrics)。重点是逐词歌词、背景效果，以及持续播放场景中的帧耗时和内存控制。

**当前提供纯 Go 核心、M2 格式解析与时间状态，以及 M1 Native UI 技术验证示例，尚无稳定的原生歌词组件或已发布版本。** M2 已由所有者确认验收通过，支持子集见 [M2 说明](docs/m2-core-formats.md)。M1 Windows 已由维护者确认验收通过；macOS/Linux 暂缓，未经实测的性能指标仍保留为未测，详见 [M1 记录](docs/m1-native.md)。`ref/` 是本地调研副本，不会随库分发。

## 当前可用

| 内容 | 状态 |
| --- | --- |
| `lyric`：只读文档、绝对时间区间、逐词/翻译/音译/对唱、行身份与源元数据 | 已实现并测试 |
| `timeline`：活动/保留高亮/焦点、间奏/曲终、双向 seek、手动跟随控制 | 已实现并测试；M2 验收通过 |
| Native UI 多行自绘、整行换行与逐词裁剪、播放控制 | `examples/native` 技术验证；完整平台验收待补 |
| 稳定原生组件、精细 cluster 高亮与弹簧滚动 | 已设计，待后续实现 |
| LRC / 增强 LRC / 明文 YRC / 明文 QRC / TTML 歌词子集解析 | 已实现并测试；[限制与语法](docs/m2-core-formats.md)，M2 验收通过 |
| 静态背景 | 原生示例可选渐变；动态背景/封面转场仍待实现 |
| CI、贡献入口、Conventional Commits、release PR 自动发版 | 配置已添加，远程启用步骤见下文 |

## 开始开发

需要 Go **1.27.3+**、Git 和 Python **3.11+**。Go 的 `GOTOOLCHAIN=auto` 可以自动取得所需版本。核心包的 import 图不依赖 mygo 或图形环境；主模块为原生示例锁定 mygo。race 检查另需可用的 C 编译器。

```sh
git clone https://github.com/xiaowumin-mark/mygo-amll.git
cd mygo-amll
go run ./examples/timeline
go run ./examples/formats -format lrc -at 5s
go run ./examples/native
python scripts/check.py
go test ./... -run '^$' -bench . -benchmem
```

示例输出两条同时活动的合成歌词。若本机尚无 race 工具链，可运行 `python scripts/check.py --no-race`；提交前必须由 CI 完成 race 检查。

原生示例无需 Bun/Node 或 WebView；Linux 需要 GTK 3 和可用图形会话。软件降级、快捷键、窗口 smoke、缓存限制与未验证项见 [M1 运行说明](docs/m1-native.md)。现阶段请从源码开发；首次正式标签生成后再提供面向使用者的 `go get ...@版本`。Go 包文档可通过 `go doc ./lyric` 与 `go doc ./timeline` 查看。

## 设计与参与

- [实现方案和验收里程碑](docs/implementation.md)
- [模块、数据流和 API 设计](docs/architecture.md)
- [性能与内存预算](docs/performance.md)
- [本次验证记录与核心基准](docs/validation.md)
- [M1 原生示例与验收记录](docs/m1-native.md)
- [M2 歌词核心与格式验收](docs/m2-core-formats.md)
- [开发规则与检查命令](docs/development.md)
- [上游快照和能力核对](docs/upstreams.md)
- [Native UI 技术决策](docs/adr/0001-native-rendering.md)
- [贡献指南](CONTRIBUTING.md)、[提交规范](docs/commit-convention.md)、[社区治理](GOVERNANCE.md)
- [发版与 GitHub 启用步骤](docs/releases.md)

欢迎从小型修复、测试向量、布局边界情况和三平台性能测量开始。新增公共接口、缓存策略或渲染后端应先提交设计提案；实现里程碑不会承诺未经测量的帧率或内存数据。

## 许可证

本项目采用 **AGPL-3.0-only**，见 [LICENSE](LICENSE)。参考 AMLL 同样采用 AGPL-3.0-only；mygo 采用 MIT。面向闭源宿主集成前，请自行评估 AGPL 的适用义务。来源、移植边界和资产规则见 [第三方声明](THIRD_PARTY_NOTICES.md)。
