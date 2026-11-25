# 架构与实现说明

## 1. 数据流

```
文件字节
  │
  ├─ dex.Load / dex.ParseContainer       识别 .dex 或 ZIP，按 classes.dex、classes2.dex… 排序
  │
  ├─ dex.Parse                            头部 → 字符串池 → type/proto/field/method → class_def
  │                                        → class_data → code_item → 指令解码
  │                                        → map_list → method_handle / call_site
  │                                        → 注解目录 / encoded_value
  │                                        → debug_info 状态机
  │
  ├─ store.Open                           按计划建表 → 单事务批量插入 → 按需建索引 → 建 8 个视图 → query_only=ON
  │
  ├─ tree.BuildGraph                      按方法描述符聚合调用边（含调用点计数）
  │
  ├─ store.Search / stats                   直接扫描模型（不物化），毫秒级定位字符串与符号
  │
  └─ 出口：CLI / HTTP / MCP（共享同一套查询模板与中英文案）
```

解析是**急切**的：`dex.Parse` 返回后，所有节都已解码为 Go 结构；`store.Open` 再把它们摊平成行。这样做的代价是打开慢、内存高，收益是查询路径没有任何隐式 I/O，行为可复现。

## 2. DEX 解析

### 2.1 关键不变量

- 所有偏移读取前做边界检查；越界返回错误而不是 panic。
- 单个类/方法损坏不会中断整个文件：记录到 `File.Warnings`，继续解析其余部分。
- 字符串统一转成 Go 字符串（UTF-8），但按 MUTF-8 规则解码（`internal/dex/reader.go`）：
  - `U+0000` → `C0 80`
  - 补充平面字符用 CESU-8 代理对
  - 孤立代理码位替换为 `U+FFFD`

### 2.2 指令解码

`internal/dex/insns.go` 维护 256 项助记符表和一个格式→长度函数：

- 长度按格式归类（10x/12x/11n/…/51l）；
- `nop` 的高字节非零时是 **payload 伪指令**，需要按 `packed-switch-data`、`sparse-switch-data`、`array-data` 的长度跳过，否则后续指令全部错位；
- 引用提取只针对带索引的格式：

| 指令 | 引用类型 | 备注 |
| --- | --- | --- |
| `const-string` / `jumbo` | string | 21c / 31c |
| `const-class`、`check-cast`、`new-instance`、`new-array`、`instance-of`、`filled-new-array` | type | 21c / 22c / 35c / 3rc |
| `iget*` / `iput*` / `sget*` / `sput*` | field | `using_type` 为 get/put，按 opcode 区间判定 |
| `invoke-*`、`invoke-polymorphic` | method | 4xcc 额外记录 proto |
| `const-method-handle` / `const-method-type` | method_handle / proto | DEX 038+ |
| `const/4`、`const/16`、`const`、`const/high16`、`const-wide/*` | 立即数 | 生成 `method_numbers` |

立即数的类型判定是启发式的：`const/high16` 与 `const-wide/high16` 常被编译器用于浮点字面量，因此分别标记为 `float`/`double`；其余按宽度标记 `byte/short/int/long`。这会影响 `method_using_numbers.number_type`，不影响值本身。

### 2.3 debug_info

`internal/dex/debug.go` 实现规范里的状态机：

- 0x00 结束；0x01/0x02 推进地址/行号；0x03/0x04 开始局部变量；0x05/0x06 结束/重启；0x07/0x08 序言/尾声；0x09 切换源文件；其余为 special opcode（地址增量 `adjusted/15`，行增量 `-4 + adjusted%15`）。
- 输出 `debug_positions`、`debug_locals`、`debug_parameters`。

### 2.4 注解与编码值

`encoded_value` 是自描述的变长结构：低 5 位是类型，高 3 位是字节数减一。解析器对数组和嵌套注解递归展开，同时记录：

- 每个节点的 `value_off/value_end_off`（可回到原始字节区间）；
- `depth` 与 `ordinal`（还原树形结构）；
- 索引型值的域（string/type/field/method/proto/method_handle）。

注解目录的布局容易写错：`class_idx`、`class_annotations_off`、`fields_size`、`methods_size`、`parameters_size`，之后才是三类条目。实现按 20 字节头处理。

## 3. SQL 物化

### 3.1 为什么用真实表而不是虚拟表

SQLite 的虚拟表接口需要 CGO 或自写 VFS，而纯 Go 驱动 `modernc.org/sqlite` 不暴露 `sqlite3_create_module`。选择"建表 + 插入"换来了：

- 完整 SQL：CTE、递归 CTE、窗口函数、`FILTER`、`group_concat`；
- 无需维护查询计划器；
- 调试简单：`sqlite_master` 里就是全部真相。

代价是内存与打开时间，已在 README 的"限制"中说明。

### 3.2 表结构与写入

- 目录集中在 `internal/store/schema.go`，`tables` 命令、HTTP `/api/tables`、MCP `list_tables` 与建表 DDL 共用同一份定义，不会漂移。
- 所有标识符在建表时加双引号，因此 `method_refs."index"` 可以存在（查询时也必须加引号）。
- 写入使用单事务 + 每表 8 行定宽批量 `INSERT`；顺序为：文件元数据 → 字符串/类型 → proto/field/method → class → code/字节码 → 注解 → 调试信息 → 布局。每个阶段只在计划要求它的表时才运行。
- 索引在数据写完后创建，避免插入时维护 B 树；只建与"这次真正物化的表"相关的索引（`idx_methods_key` 只在某个 `*_calls` 视图要用时才建）。

### 3.2.1 引用表与视图

`method_calls`、`method_field_accesses`、`method_used_types`、`method_strings` 的列与 0.x 完全一致，但底层是四张只存整数索引的内部表（`_call_sites` 等），公开名字是 join `methods`/`fields`/`types`/`strings` 的视图。

理由很直接：一条调用指令会在每一行重复 caller 的 descriptor，K-9 的 32.7 万行里有 104 MB 这样的重复文本。改用索引后目录从 182 MB 降到 86 MB，时间不变——省下的写放大被"查询时还要物化 methods"抵消。

视图必须是**简单视图**（无聚合/`DISTINCT`/`LIMIT`），SQLite 才会把它展开进外查询，让 `WHERE callee = ?` 下推到底表并用上索引。因此 caller 列写成裸的 `m.descriptor` 而不是 `COALESCE(m.descriptor, '')`：包一层函数，索引就用不上了。`_call_sites.callee` 保留一列 descriptor，正是为了让"谁调用了 X"这个最热的问题不需要先扫 `methods`。

### 3.3 只读保护

两层：

1. `PRAGMA query_only=ON`：即使语句绕过前缀检查，SQLite 也会拒绝写入。
2. `Guard()`：剥离注释、检查首个关键字（`SELECT/WITH/VALUES/EXPLAIN`）、检测字符串字面量之外的分号，阻止多语句。

`limit` 的实现是把原语句包成子查询：`SELECT * FROM (<sql>) LIMIT n+1`，多取一行用于判断 `truncated`。

### 3.4 NULL 语义

- 引用型方法：`access_flags/opcode_count/opcode_hex/method_kind` 为 NULL；已声明但无 code_item（abstract/native）为 `0` 与空串。
- `superclass_descriptor`、`source_file`、`value_index` 等按 DEX 的 `NO_INDEX` 映射为 NULL。
- Go 的 `nil` 直接写入 SQLite，读出后转成 JSON `null`。

## 4. 调用图与树

`tree.BuildGraph`：

1. 先把每个 DEX 的 `method_ids` 播种为节点，保证"叶子方法"也能作为根；
2. 扫描每个 `code_item` 的 method 引用，按 `(caller, callee)` 聚合调用点数量；
3. 同时维护正向（callees）与反向（callers）邻接表，按调用点数降序、描述符升序排序，保证输出稳定。

`Build` 使用 DFS：

- `depth` 限制层数；`maxNodes` 限制总量，超限置 `truncated` 并停止扩展；
- 用当前路径集合检测环：命中时生成标记 `cycle` 的节点但不继续递归；
- 节点携带 `file_id/method_idx`，便于回到 `code_items` 或 `smali`。

输出三种形态：缩进文本、JSON、Graphviz DOT。

## 5. 协议层

### HTTP

`internal/api` 是无状态 JSON 层，数据集注册表加锁保护。`serve` 启动时若有 `--input` 会预加载一个数据集（id `d1`）；也可以运行时 `POST /api/datasets` 追加。调用树按数据集路径缓存 `Graph`，避免重复建图。

### MCP

`internal/mcp` 手写 JSON-RPC 2.0，按行分隔：

- 通知（无 `id`）不回复；
- `tools/call` 的业务错误放在 `result.isError=true` 的 content 中，而不是 JSON-RPC error，符合 MCP 约定；
- 参数用 `json.RawMessage` 延迟解析，单个工具的参数错误不会影响其他工具。

### Java

- `DexsqlClient` 只用 `java.net.http.HttpClient` + Jackson，不引入框架；
- `DexsqlProcess` 负责在随机空闲端口拉起 `serve`，轮询 `/api/health`，失败时把日志尾部带进异常；
- GUI 的所有网络调用都在后台 `Task` 中执行，UI 线程只做渲染。

## 6. 测试策略

| 层 | 方式 |
| --- | --- |
| DEX 解析 | `internal/dextest` 用代码构造一个 672 字节、结构合法的 DEX：1 个类、1 个方法、1 个字段、1 条调用、1 条调试信息 |
| SQL | 打开该 DEX，断言实体视图、引用视图、join、只读守卫、limit/truncated、目录完整性 |
| 调用树 | 断言 callers/callees、深度截断、环检测、节点预算 |
| Java 客户端 | 本地 `com.sun.net.httpserver` 桩，覆盖 JSON 映射、`class` 字段、错误转换 |
| 端到端 | `EngineIntegrationTest` 真实启动 Go 二进制，走 HTTP 查询/建树/smali；二进制缺失时自动跳过 |

合成 DEX 的好处是**可读、可断言**：每个字节都有明确来源，失败时容易定位是解析器还是测试数据的问题。局限是覆盖面窄，真实 APK 的混淆、多 DEX、异常表仍需额外验证。

## 7. 性能分析

当前打开阶段的主要成本：

1. 全量读取 ZIP 内的 DEX（不可避免）；
2. Go 侧逐条解码指令（O(指令数)）；
3. SQLite 单事务逐行插入（最大瓶颈，约占总时间 60–80%）；
4. 建索引。

优化顺序应当是：批量多行 `INSERT` → 并行解析各 DEX → 延迟物化冷表（如 `debug_*`）→ 可选持久化缓存。查询阶段依赖 SQLite 索引，典型点查在毫秒级。

## 8. 扩展指南：新增一张表

1. 在 `internal/store/schema.go` 的 `Catalog` 增加条目（列名 + 逻辑类型）；
2. 在 `internal/store/load_*.go` 增加写入循环，列顺序必须与 `Catalog` 一致；
3. 如需索引，加到 `indexDDL`；
4. `go test ./internal/store` 会自动检查目录与物理表是否一致。

因为 DDL、文档和协议都从 `Catalog` 派生，新增表不需要改 CLI、API 或 MCP。
