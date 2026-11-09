# 提交与 PR 标题规范

采用 Conventional Commits 1.0.0。默认 squash merge，PR 标题就是最终提交标题；CI 检查这一标题，发版工具据此生成版本和 changelog。

```text
type(scope)!: concise imperative description
```

`scope` 和 `!` 可选。类型小写，冒号后一个空格，描述应简明指出改动。标题可中文或英文。检查器要求标题为单行、无首尾空白、长度不超过 100 字符，且 scope 使用小写字母、数字、`.`、`_`、`-`、`/`；需要更长说明放正文。

| 类型 | 适用内容 | 默认版本影响 |
| --- | --- | --- |
| `feat` | 新功能或公共能力 | minor |
| `fix` | 行为修复 | patch |
| `perf` | 有证据的性能改善 | patch |
| `docs` | 文档 | 单独通常不触发发布 |
| `test` | 测试/基准 | 单独通常不触发发布 |
| `refactor` | 不改变外部行为的重构 | 单独通常不触发发布 |
| `build`、`ci`、`chore` | 构建、自动化、维护 | 单独通常不触发发布 |
| `style` | 只改格式 | 单独通常不触发发布 |
| `revert` | 回滚 | 由 release-please 识别原改动，检查生成结果 |

推荐 scope：`lyric`、`timeline`、`format`、`layout`、`render`、`motion`、`background`、`native`、`deps`、`release`。

```text
feat(lyric): add read-only document snapshots
fix(timeline): retain overlapping lines after seek
perf(layout): reuse visible line buffers
docs: explain native rendering resource ownership
feat(native)!: change playback clock input
```

破坏性变更必须用 `!`，并在正文写 `BREAKING CHANGE:` 和迁移方式。初期 `v0.x` 的 breaking 变化递增 minor；达到 `v1` 后按 SemVer 递增 major。不能把破坏性改动藏在 `chore` 中。

提交正文说明问题、行为和验证；不要只重复标题。一个 PR 保持一个主题，独立的格式调整与逻辑改动分开。贡献者无需给本地每个 WIP 提交改名，但可审阅的最终 squash 标题必须合规。release PR 标题由工具生成并同样检查；具体规则以仓库检查器与发版配置为准。
