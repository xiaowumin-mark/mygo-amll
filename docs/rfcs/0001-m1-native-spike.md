# RFC 0001：M1 的示例内原生验证边界

- 日期：2026-11-09。
- 状态：所有者已确认 M1 Windows 验收通过并授权提交；macOS/Linux 暂缓，尚未提交远程设计 issue。
- 依据：[ADR 0001](../adr/0001-native-rendering.md)、[M1 范围与验收](../m1-native.md)。

M0 的公开纯 Go 核心不足以验证真实 Native UI 的文字、裁剪和调度。本次以 `examples/native` 中非导出的实现检验固定 mygo 提交，不提前发布 `native/mygo`、`layout`、`render` 公共 API。数据/时间核心的依赖方向保持不变；一个 module 统一锁定依赖。

选择整行保留布局和公开 SelectionRects，避免逐词 shaping 破坏文字。为能审计公开布局隐藏的 cluster，冷路径再完整 Shape 一次；相比直接接触上游 internal，代价是有界冷加载开销。跨 cluster 的细粒度视觉策略延期至 M3，当前保守地按词结束填色。

选择 UI 线程拥有、32 条目/16 MiB 估算双预算缓存；本帧引用固定在同一预算内。比全曲预排版减少长歌词准备与内存，但 seek/resize 有冷缓存成本，极大单行采用提示退化。精确宽度失效优先保证几何正确；宽度分桶复用待以后有测量证据再加入。

所有播放状态变更在 view/输入回调；Draw 仅读时间锚点及缓存并申请仅重绘。没有库级 goroutine；可选原生 smoke 的辅助任务在关闭取消。颜色背景先用静态渐变，不引入 shader/Canvas 或 GPU 生命周期承诺。

新增 mygo 为已验证固定伪版本，MIT notice 随仓保留；实际传递模块见 go.mod/go.sum。它带来字体系统与原生平台运行条件，核心 `lyric` / `timeline` 的 import 图仍无 UI、音频或 I/O。

验收包含确定性时钟、Unicode 几何、缓存与帧引用预算、UI Tester 和真实窗口；未有 macOS/Linux 机器不能认定通过三平台验收。2026-11-09，所有者确认 Windows 范围通过并授权提交，其他平台及扩展性能实测延期；这不把技术验证示例变成完成的可嵌入组件或预发布版本。
