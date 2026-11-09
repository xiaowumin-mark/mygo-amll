# mygo-amll

使用 [mygo Native UI](https://github.com/egoist/mygo) 与自定义 GPU 绘制实现的可嵌入歌词库，行为目标参考 [Apple Music-like Lyrics](https://github.com/amll-dev/applemusic-like-lyrics)。重点是逐词歌词、背景效果，以及持续播放场景中的帧耗时和内存控制。

**当前处于仓库初始化阶段，尚无 Native UI 歌词组件，也没有已发布版本。** 已提供可运行的纯 Go 歌词文档与时间索引、测试和基准，以及实现设计和社区自动化配置。`ref/` 是本地调研副本，不会随库分发。

## 当前可用

| 内容 | 状态 |
| --- | --- |
| `lyric`：只读文档、绝对时间区间、基础逐词/翻译/音译/对唱元数据 | 已实现并测试 |
| `timeline`：活动行查询、重叠区间、双向 seek、复用结果缓冲区 | 已实现并测试 |
| Native UI 自绘、文字布局、逐词高亮、弹簧滚动 | 已设计，待实现 |
| LRC / ESLRC / YRC / QRC / TTML 解析 | 已规划，待实现 |
| 动态背景、封面转场、GPU 缓存、平台示例 | 已规划，待实现 |
| CI、贡献入口、Conventional Commits、release PR 自动发版 | 配置已添加，远程启用步骤见下文 |

## 开始开发

需要 Go **1.27.3+**、Git 和 Python **3.11+**。Go 的 `GOTOOLCHAIN=auto` 可以自动取得所需版本。纯 Go 核心无需 mygo、Bun、Node 或图形环境；race 检查另需可用的 C 编译器。

```sh
git clone https://github.com/xiaowumin-mark/mygo-amll.git
cd mygo-amll
go run ./examples/timeline
python scripts/check.py
go test ./... -run '^$' -bench . -benchmem
```

示例输出两条同时活动的合成歌词。若本机尚无 race 工具链，可运行 `python scripts/check.py --no-race`；提交前必须由 CI 完成 race 检查。

现阶段请从源码开发；首次正式标签生成后再提供面向使用者的 `go get ...@版本` 和 Native UI 示例。Go 包文档可通过 `go doc ./lyric` 与 `go doc ./timeline` 查看。

## 设计与参与

- [实现方案和验收里程碑](docs/implementation.md)
- [模块、数据流和 API 设计](docs/architecture.md)
- [性能与内存预算](docs/performance.md)
- [本次验证记录与核心基准](docs/validation.md)
- [开发规则与检查命令](docs/development.md)
- [上游快照和能力核对](docs/upstreams.md)
- [Native UI 技术决策](docs/adr/0001-native-rendering.md)
- [贡献指南](CONTRIBUTING.md)、[提交规范](docs/commit-convention.md)、[社区治理](GOVERNANCE.md)
- [发版与 GitHub 启用步骤](docs/releases.md)

欢迎从小型修复、测试向量、布局边界情况和三平台性能测量开始。新增公共接口、缓存策略或渲染后端应先提交设计提案；实现里程碑不会承诺未经测量的帧率或内存数据。

## 许可证

本项目采用 **AGPL-3.0-only**，见 [LICENSE](LICENSE)。参考 AMLL 同样采用 AGPL-3.0-only；mygo 采用 MIT。面向闭源宿主集成前，请自行评估 AGPL 的适用义务。来源、移植边界和资产规则见 [第三方声明](THIRD_PARTY_NOTICES.md)。
