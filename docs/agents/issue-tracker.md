# Issue tracker: Local Markdown

本仓库的任务和规格保存在 `.scratch/`，使用本地文件管理。

## 文件约定

- 每个功能一个目录：`.scratch/<feature-slug>/`。
- 规格：`.scratch/<feature-slug>/spec.md`。
- 实现任务：`.scratch/<feature-slug>/issues/<NN>-<slug>.md`，从 `01` 编号，每张任务独立成文件。
- 分诊状态写在任务文件顶部附近的 `Status:` 行，取值见 `triage-labels.md`。
- 评论和讨论追加到文件末尾的 `## Comments` 下，保留已有历史。

## 技能操作映射

- “发布到任务跟踪系统”：在上述路径创建文件，按需创建目录。
- “获取任务”：读取用户提供的路径；只有编号时，在对应功能目录中定位文件。

## Wayfinding 操作

- 决策地图：`.scratch/<effort>/map.md`，记录 Notes、Decisions-so-far 和 Fog。
- 子任务：`.scratch/<effort>/issues/NN-<slug>.md`，从 `01` 编号，正文记录待解决的问题。
- `Type:` 记录 `research`、`prototype`、`grilling` 或 `task`。
- `Blocked by: NN, NN` 记录阻塞关系；列出的任务全部为 `resolved` 后解除阻塞。
- 从尚未解决、未被认领且无阻塞的任务中，按编号选择下一个任务。
- 开始处理前写入 `Status: claimed` 并保存。
- 完成后在 `## Answer` 下记录答案，设为 `Status: resolved`，并在地图的 Decisions-so-far 中追加结论摘要和任务链接。
