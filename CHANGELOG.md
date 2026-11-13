# Changelog

## 0.1.0 (2026-11-13)


### ⚠ BREAKING CHANGES

* **format:** `Line` / `Word` 新增字段，外部应使用有字段名的结构字面量。LRC offset 改为 AMLL metadata-only 语义，并对齐翻译/背景/末行规则；格式解析器显式保留非负历史源时间（含 point、反向、越行和非单调），调用者不可再假设所有解析结果的词都按时间排序或位于行内。默认文档构造器的严格规则保持不变。首语言简化视图不构成无损 TTML 往返承诺。
* **lyric:** lyric.Line gains ID, Agent and Language fields. External unkeyed struct literals must migrate to field-named initialization.

### Features

* **format:** add bounded plaintext QRC parsing ([#10](https://github.com/xiaowumin-mark/mygo-amll/issues/10)) ([3eda023](https://github.com/xiaowumin-mark/mygo-amll/commit/3eda0235d4175353341455445544c9ac8b0e1951))
* **format:** add bounded plaintext YRC parsing ([#9](https://github.com/xiaowumin-mark/mygo-amll/issues/9)) ([abe38e5](https://github.com/xiaowumin-mark/mygo-amll/commit/abe38e53232129274abdfe1c0e75408cb761e8ed))
* **format:** add TTML profile and complete M2 integration ([#11](https://github.com/xiaowumin-mark/mygo-amll/issues/11)) ([24d0740](https://github.com/xiaowumin-mark/mygo-amll/commit/24d07407585941422bc99cb5f267a21f24e86a08))
* **format:** align parsing with the AMLL lyric corpus ([c8e4a07](https://github.com/xiaowumin-mark/mygo-amll/commit/c8e4a079b89432f30146eb037e730ea1ab4eb145))
* initialize native lyrics library foundation ([e740219](https://github.com/xiaowumin-mark/mygo-amll/commit/e74021976fc9585f360c6998c5838da439ed0c9f))
* **lyric:** add M2 visual states and LRC parsing ([#8](https://github.com/xiaowumin-mark/mygo-amll/issues/8)) ([471e728](https://github.com/xiaowumin-mark/mygo-amll/commit/471e728661591214e4c40724261aea6520618672))
* **native:** add M1 Native UI validation example ([#7](https://github.com/xiaowumin-mark/mygo-amll/issues/7)) ([df6ac1a](https://github.com/xiaowumin-mark/mygo-amll/commit/df6ac1a7d586840281c908fb2c7301b0566ed3a0))
* **native:** complete M3 AMLL rendering ([e68f68d](https://github.com/xiaowumin-mark/mygo-amll/commit/e68f68dabb6377d0305486c0e051f471282e9e47))

## 更新记录

当前仓库尚未发布版本。首次发布及后续版本由 release-please 根据合并到默认分支的 Conventional Commits 自动维护本文件。
