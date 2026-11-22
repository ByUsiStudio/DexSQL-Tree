# 原项目分析：LSPosed / dexsql-release

> 分析对象：<https://github.com/LSPosed/dexsql-release>（README、文档站、benchmark 材料）。
> 目的：明确它解决了什么问题、用了什么取舍，以及 `dexsql-tree` 哪些地方应当对齐、哪些地方应当不同。

## 1. 产品定位

DexSQL 的自我描述是 "SQL for Android binaries"：把 APK/DEX 中的类、方法、字符串和调用引用暴露成关系，让 AI Agent 通过 MCP 或命令行用一条 SQL 组合多个条件，只返回结构化结果。

它瞄准的不是"反编译给人看"，而是"给 Agent 一个可反复查询的数据库"。这带来三个设计后果：

1. **接口优先**：单可执行文件同时是 CLI 和 stdio MCP server，不需要 GUI、不需要 JVM。
2. **一次打开、多次查询**：解析结果常驻内存，后续查询低延迟。
3. **结果可复用**：SQL 可以存成脚本，换一个版本的应用再跑一遍。

## 2. 能力面

### CLI

| 命令 | 含义 |
| --- | --- |
| `dexsql query --input app.apk --sql "..."` | 一次性查询；默认只准备 SQL 需要的数据 |
| `dexsql query --input app.apk --interactive` | 终端交互会话 |
| `dexsql tables --input app.apk` | 显示内置表名、列与 SQL 类型 |
| `dexsql prepare-cache --input app.apk --output dir` | 解析并写出 `cache.dxc2` |
| `dexsql query-cache --cache-file ... --sql "..."` | 查询持久缓存 |
| `dexsql decode --input app.apk --method 'Lcom/x;->y(I)V'` | 反汇编为 smali |
| `dexsql mcp` | stdio MCP |
| `dexsql extract licenses` | 导出第三方许可 |

### 数据模型

- 键空间：多数表以 `file_id` 划分；字符串以 `(string_pool_id, string_idx)` 为键，因为 041 容器内多个逻辑 DEX 可能共享字符串池。
- 严格区分**引用**与**定义**：
  - `methods` 是所有被引用的方法；`class_methods` 是类自己声明的方法。
  - 引用行的 `access_flags/opcode_count/opcode_hex/method_kind` 为 `NULL`。
- 提供三档抽象：
  1. 语义视图：`classes`、`methods`、`method_calls`、`method_strings` 等；
  2. 半解析表：`method_refs`、`method_numbers`、`code_items`；
  3. 原始布局：`map_items`、`id_items`、`type_list_entries`、`class_defs`。
- 覆盖范围很完整：注解（含嵌套值树）、debug_info 状态机（positions/locals/opcodes）、异常表、method handle、call site、encoded array、hiddenapi。

这套模型的价值在于**分层的可解释性**：普通查询用视图，深入排查时可以一路下钻到字节偏移。

### 查询语义

- 只接受 SQLite 内置函数的一个子集；超集函数在解析期直接拒绝（`function call is outside the DexSQL read-only subset`），避免"看起来能跑、结果不对"。
- 提供 `dex_java_name()`：把 `Lcom/f/B;` 变成 `com.f.B`，`[I` 变成 `int[]`。
- 支持窗口函数、`FILTER`、`group_concat(... ORDER BY ...)`。
- 文本以 MUTF-8 存储、在输出边界解码。

### 性能叙事

官方 benchmark 的关键数字（来自 README）：

| 场景 | DexSQL | 对比 |
| --- | --- | --- |
| 抖音 40.7.0（343 MiB，56 DEX）打开一次 + 10 个分析目标 | 0.71 s | rasc 1.69 s；DexDec 16 s；其余 75–449 s |
| 单次打开成本 | 0.38 s | rasc 无打开但每步重读 APK |
| QQ 9.3.55 固定 8 条查询 | 各类查询均领先 | 部分工具 60 s 未完成 |

这些数字说明两件事：

1. 直接读 DEX + 复用数据集，比"启动 JVM + 反编译整个 APK"快一个数量级。
2. "打开成本"和"每步成本"要分开看：对只问一次的场景，轻量重读可能更快；对多轮 Agent 会话，常驻数据集优势明显。

### 分发与许可

- 二进制按 **DexSQL Binary License 1.0** 发布：允许个人/商业使用、集成、二进制再分发，但**不提供源码**。
- 安装 skill、benchmark 材料与文档是 MIT。
- 这对想审计解析行为、做二次开发、或把能力嵌进自家产品的团队是硬限制。

## 3. 可借鉴的设计

| 设计 | 为什么值得保留 | 在 dexsql-tree 中的对应 |
| --- | --- | --- |
| `file_id` 作为一级分区键 | 多 DEX 场景下所有 join 都需要它 | 保留 |
| 引用/定义分离 | 避免把"引用到的方法"误当成"实现" | 保留，`methods` 与 `class_methods` 分表 |
| 三层抽象（视图/中间表/原始布局） | 兼顾易用与可下钻 | 保留，55 张表 |
| 只读 SQL 子集 | 防止误写、保证可预测 | 采用 SQLite `query_only` + 白名单 |
| MCP 与 CLI 同体 | Agent 与人都能用 | 保留 |
| 把 smali 作为查询结果的补充 | 定位后立刻看指令 | `smali` 命令与 API |
| 明确的 `tables` 自描述 | Agent 能自己发现 schema | `tables --json`、MCP `list_tables` |

## 4. 明确不照搬的部分

| DexSQL 做法 | 问题 | dexsql-tree 的选择 |
| --- | --- | --- |
| 闭源二进制 | 无法审计、无法修改、无法嵌入 | MIT 全源码 |
| 自研列式 + SQL 子集 | 性能好，但实现成本高、SQL 兼容面窄 | 内存 SQLite，完整 SQL（含 CTE、窗口函数） |
| 一次性"只准备 SQL 所需数据" | 需要复杂依赖分析 | 打开时全量物化，行为可预测 |
| `cache.dxc2` 私有格式 | 无法跨工具复用 | 暂不实现，路线图改为开放的 `.dxsql` |
| 函数白名单 | 安全但限制多 | 依赖 SQLite 完整函数集，只做只读约束 |

**取舍的本质**：DexSQL 用工程复杂度换性能；`dexsql-tree` 用内存换实现透明度与 SQL 表达力。两者服务的目标不同，不构成替代关系上的优劣判断。

## 5. 对我们的启示

1. **Schema 是产品**。表名、列名、NULL 语义一旦公开，就是契约；`dexsql-tree` 因此尽量对齐命名，降低迁移成本。
2. **Agent 需要自描述**。`tables --json` + `describe_table` 比写死在 prompt 里更可靠。
3. **性能瓶颈在打开阶段**。如果要在大型 APK 上接近 DexSQL，优先做并行解析与批量写入，而不是优化单条 SQL。
4. **调用链是高频需求**。原项目让用户用自连接表达，`dexsql-tree` 把它产品化为 `tree`，并加上环检测与节点预算。
5. **许可即边界**。闭源二进制适合"用"，不适合"改"；这正是本项目存在的理由。
