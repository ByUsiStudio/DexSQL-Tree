# dexsql-tree 0.3.0

0.2.0 解决的是"查询太麻烦"，0.3.0 解决的是"打开太慢"。核心变化是**磁盘目录缓存**：同一个包第二次打开从数十秒降到亚秒。

## 亮点

### 1. `--cache`：再次打开只要 0.5 秒

```bash
dexsql-tree query --input vlc.apk --cache --sql "SELECT count(*) FROM methods"
# 首次 32.7 s，再次 0.55 s
```

- 缓存键 = 绝对路径 + 大小 + 修改时间 + 引擎版本 + 档位 + `--debug`，改任何一项都会重建。
- 缓存命中时**仍然完整解析**（调用树、smali、search 都依赖内存模型），只跳过 SQLite 物化，所以功能没有降级。
- 物化先写到 `<key>.db.tmp<pid>`，校验后 rename 发布，读者永远看不到半成品；损坏或截断的目录会被丢弃并重建。
- 新增子命令：`cache list`、`cache clear`、`cache path`、`cache info --input app.apk`。
- 位置默认是系统缓存目录，可用 `DEXSQL_TREE_CACHE` 或 `--cache-dir` 覆盖；被中断的临时文件在 1 小时后自动回收。

### 2. `--profile fast|full`：默认少做一半无用功

`fast`（新默认）跳过注解相关表，其余表结构完全一致；需要读 `@Keep`、`@JsonProperty` 这类注解时加 `--profile full`。

| 样本 | fast | full |
| --- | --- | --- |
| VLC 46 MB | 32.7 s | 39.4 s |

### 3. 写入路径：8 行定宽批量插入

逐行 `Exec` 改成每 8 行一条 `INSERT`，K-9 冷启动 16.7 s → 11.6 s（-30%）。批次不能更大：纯 Go 驱动对每个绑定参数都有一次反射开销，128 行的批次比不批量还慢。这个最优点写在了代码注释里，避免以后有人"优化"回大步长。

### 4. 多 DEX 并行解析

`--jobs N`（默认 CPU 核数）把 `classes.dex`、`classes2.dex`… 并发解码，结果按原顺序落位，`file_id` 与串行完全一致（有测试覆盖）。6 DEX 的 VLC 从 33.3 s 到 30.9 s——解析只占小包总时间的 3–5%，这项是为"解析成为瓶颈"的将来准备的。

### 5. DEX 041 容器拆分

DEX 041 把多个逻辑 DEX 放在一个容器里。现在会拆成 `classes.dex#1`、`#2`… 分别解析，`dex_files` 一行对应一个逻辑 DEX，`file_id` 稳定。

### 6. `hiddenapi_flags` 表

解析 `hiddenapi_class_data_item`（map_list 类型 0xF000），按 class_defs 顺序读 uleb128 标志位，输出 `restriction`（`sdk`/`unsupported`/`blocked`/`core-platform`/`test`）。只写入有限制的成员，表因此很小。解析是保守的：越界立即停止并记 warning，不会产出假数据。

```sql
SELECT restriction, count(*) FROM hiddenapi_flags GROUP BY restriction;
```

### 7. 接口层

- HTTP 新增 `GET /api/search?keyword=X&kind=class`、`GET|DELETE /api/cache`；`/api/health` 返回 `profile` 与 `cache`。
- MCP 新增 `describe_input`：只报输入的规模，不建目录，便于 agent 先探后查。
- `list_datasets` 现在带 `profile` 与 `cached`。

### 8. Java 客户端与 GUI

**客户端**

- 所有请求可走 `queryAsync` / `treeAsync` / `smaliAsync` / `searchAsync`，共享一个 2–8 线程的 daemon 池；`HttpClient` 也用同一个池。
- 每个方法都能显式传 `dataset`，不再依赖可变字段，多线程调用安全。
- 新增 `healthInfo()`、`search()`、`recipe()`、`cache()`、`clearCache()`。

**GUI**

- 结果分页（每页 100/200/500/1000），5000 行的答案只渲染一屏单元格。
- 查询可取消，取消走线程中断，状态栏显示 "Cancelled"。
- 错误改为结果区内联横幅，不再弹窗打断操作。
- 新增 Search 页：关键词 + 类型过滤，双击方法直接建调用树，双击类列出它的方法。
- 顶部连接状态指示灯、`engine` 版本、worker 线程数、缓存对话框（可清空）。
- 快捷键：`Ctrl+Enter` 运行、`Ctrl+K` 搜索、`Ctrl+O` 打开、`F5` 重连。

## 实测

Linux x64 / 12 核 / Go 1.21，`--profile fast`：

| 样本 | 0.2.0 | 0.3.0 冷启动 | 0.3.0 再次打开 |
| --- | --- | --- | --- |
| K-9 11 MB | 18.7 s / 1.45 GB | 12.0 s / 500 MB | 0.44 s / 230 MB |
| VLC 46 MB | 55.7 s / 3.87 GB | 32.7 s / 1.32 GB | 0.55 s / 860 MB |

`--disk` 下 VLC 内存 1.31 GB、时间 30.9 s；`--profile full` 39.4 s。

## 兼容性

- 命令行兼容：`--cache`、`--profile`、`--jobs`、`--cache-dir` 都是新增，旧命令行为不变（除默认档位变为 `fast`，注解表默认空）。
- 表数量 55 → 56（新增 `hiddenapi_flags`），既有表名与列序未变。
- Java 库：`TableInfo` 增加 `kind` 字段（构造器变为 4 参），`DexsqlClient` 的旧方法签名保留。
- 缓存目录格式属于内部实现，跨版本升级时会因版本号变化自动重建，不会读到旧结构。

## 已知限制

- 46 MB 样本首次打开仍需约 33 s，瓶颈是 170 万行逐行写入，调参已到极限；下一步需要按需物化（只建 SQL 引用的表）。
- `hiddenapi_flags` 没有在大量商业包上做过统计校验。
- JavaFX 端未在物理显示器上逐项点测，只有编译与端到端验证。

## 下一步（0.4.0）

新手体验优先：首次启动引导、示例查询一键运行、错误信息带"下一步"建议、表说明中英双语、GUI 首次使用提示。技术上做按需物化，把大包首次打开压到 10 s 内。
