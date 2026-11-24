# dexsql-tree 0.4.0

0.3.0 解决的是"第二次打开太慢"，0.4.0 解决的是**第一次打开写得太多的行**。

## 问题的形状

一个 4.6 MB 的 DEX（Guise 2.1.0）分项耗时是这样的：

```
解析 DEX      0.26 s
插入 117 万行 7.74 s
建 11 个索引  1.47 s
```

解析只占 2%。真正的成本是物化：一份 4.6 MB 的输入被写成 116.9 万行（约每 KB DEX 253 行），库文件 90.7 MB。而一次典型的 agent 查询只读两张表——`method_calls` 加 `methods` 合计 16.4 万行，占全部行数的 14%。

所以这一版不调参数，改的是**写什么、什么时候写**。

## 按需物化

`query` 现在先拿到 SQL，再决定建哪些表：

1. 词法扫描 SQL，跳过注释、单引号字符串、`--`/`/* */`，收集裸标识符、双引号/反引号/方括号标识符；
2. 与 `Catalog` 的 56 个表名匹配，`v_methods`、`v_fields`、`v_overview` 按 DDL 展开成基表；
3. 只对这些表执行加载步骤，`ins` 对计划外的表直接返回，索引也只建这些表。

```
SELECT * FROM methods                     -> methods
SELECT * FROM v_methods                   -> methods, types
SELECT count(*) FROM method_calls          -> method_calls
SELECT 1                                   -> （不建表）
```

几个刻意的取舍：

- **不做语法分析**。词法是保守的：CTE 或别名撞上表名只会多建一张，不会漏。
- **不把 `SELECT *` 当歧义**。星号只影响列，`FROM` 后面的基表一定会被识别；把 `SELECT * FROM methods` 退回全量会毁掉最常见的查询。
- **不静默返回 0 行**。`Query` 会核对语句读到的表是否真的建过；没建就报 `relation X was not materialised; rerun with --all-tables`。HTTP/MCP 的长驻进程则反过来：第一次遇到就补建（见下）。
- **缓存键带上表集合**。完整目录可以回答它的任何子集；部分目录不会被拿去回答更大的查询。已有的完整目录优先复用，所以升级后第一次查询命中旧缓存仍然很快。

### 服务端改为懒物化

`serve` 和 `mcp` 在打开时不知道下一条语句是什么。它们现在开一个可写目录、不建任何表，收到语句时把缺的表补进去（`PRAGMA query_only` 在写入窗口内临时关闭，`Guard` 仍然拒绝非 SELECT）。`serve --input app.apk` 的启动时间从"物化完才监听"变成"解析完就监听"。

对 agent 的 MCP 会话，`open_dataset` 现在是解析（约 0.3 s），第一次 `query` 付它读到的那几张表，后续查询复用。

懒物化与 `--cache` 的交集要说清楚：`serve`/`mcp` 只在 `--profile full` 时挂载已有的完整目录。`fast` 目录缺三张派生表，而只读的缓存文件不能补建，挂上去反而会让一条合法查询报错。

## fast 档去掉冗余的派生表

实测行数：`method_refs` 30.2 万（其余引用表的超集）、`method_using_numbers` 5.8 万（与 `method_numbers` 完全重复）、`id_items` 7.9 万（原始偏移，只有取证级查询用）。`fast` 档默认不再写这三张，行数从 117 万降到约 73 万。

和注解表一样，表结构保留。不同的是：**语句点名这三张表时仍会构建**。0.3.0 的注解表在 `fast` 下查到 0 行，是因为省掉的是展开成本；这三张表只是同一批数据的另一种切法，按需构建既便宜又不会给出错误答案。

`--profile full` 的注解展开也已经是按需的：语句不引用注解表，就不走注解树。K-9 上 `full` + `count(*) FROM annotations` 0.44 s，`full` + `count(*) FROM methods` 0.94 s。

## 索引与写入路径

- 11 个索引里删掉 5 个：`methods(descriptor)`、`classes(descriptor)`、`fields(descriptor)`、`strings(value)`、`method_strings(value)`。`LIKE '%x%'` 一个也用不上，留着只花建库时间和磁盘。保留的 6 个都服务等值匹配（`method_calls.callee`、`method_calls.method`、`methods.class_descriptor`、`types(file_id, descriptor)`、`class_methods.method_idx`、`method_refs(index_kind, "index")`）。
- 建表前设 `PRAGMA page_size=16384`。K-9 的 `method_calls` 目录：2.90 s → 2.68 s，195.8 MB → 190.1 MB。
- `method_calls`、`method_strings`、`methods`、`strings`、`types` 等热点表不再走 `ins(table, v1, v2, ...)`：可变参数每行都会分配一个 `[]any`，现在直接 `append` 进批次缓冲区。单阶段 1.54 s → 1.37 s。
- `query` 物化完成后释放解析模型并 GC 一次。SQL 不需要它，峰值 RSS 降约四分之一。

## 接口与安全

- `serve` 新增 `--token SECRET`：`/api/*` 需要 `Authorization: Bearer` 或 `?token=`，静态页面不需要（浏览器打开 `/?token=...` 即可）。监听非回环地址却没有 token 时会打印警告。
- `/api/search` 的空结果从 `"rows": null` 改为 `[]`。
- Java 客户端新增 `new DexsqlClient(url, timeout, token)`，GUI 顶栏多了一个 token 输入框。

## 解析器加固

补 fuzz 时（`FuzzParseContainer`）第一分钟就踩到一个真实问题：头部/映射表里的计数是攻击者可控的，`parseStrings` 在第一次边界检查之前就按 `string_count` 分配切片，一个 441 字节的输入要申请 28 GB，进程直接 OOM。现在所有按计数分配的节（strings、type/proto/field/method ids、class_defs、map_list、method_handles、call_sites、class_data 成员、encoded_array）都先做 `fits(off, count, size, len)` 检查。7.2M 次执行无崩溃。

## 实测

Linux x64，`--profile fast`，冷启动，无 `--cache`：

| 样本 | 0.3.0 任意查询 | 0.4.0 `methods` | 0.4.0 `method_calls` | 0.4.0 `SELECT 1` |
| --- | --- | --- | --- | --- |
| K-9 11 MB / 64,586 方法 | 11.7 s / 1.34 GB | 0.79 s / 282 MB | 2.6 s / 556 MB | 0.36 s / 207 MB |
| VLC 46 MB / 270,153 方法 | 31.8 s / 3.52 GB | 2.34 s / 1.07 GB | 5.7 s / 1.69 GB | — |

全量（`--all-tables`）K-9：10.4 s / 927 MB，比 0.3.0 快 11%。

## 兼容性

| 变化 | 影响 |
| --- | --- |
| 只物化语句读到的表 | 一条语句里同时引用多张表时，它们都会被建；引用未建表会报错而不是返回空 |
| `--all-tables` | 新开关，绕过计划，行为等同 0.3.0 的"打开即全量"（`fast` 仍不写注解行） |
| 缓存键包含表集合 | 旧缓存（0.3.0 的键）不会被读取，因为版本号也在键里；首次查询会重建 |
| `fast` 不写三张派生表 | 默认查询看不到 `method_refs` 等行；点名即建 |
| `serve --token` | 可选，不传时行为不变 |

## 下一步

- `fast` 档的注解表也支持"点名即建"，让默认档在遇到注解查询时自愈，而不是提示用户换档。
- `debug_positions` 打包：一个方法一行、内容用 varint 二进制块，725 万行降到 6.8 万行，`--debug` 从 50 s 降到个位数。
- 0.5.0 回到新手体验：`start` 子命令、任务导向模板、报错带建议、GUI 空态示例卡片。见 [roadmap-0.5.0.md](roadmap-0.5.0.md)。
