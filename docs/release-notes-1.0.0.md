# dexsql-tree 1.0.0

1.0.0 不是"功能更多"，而是把 0.x 里三个会随使用规模放大的问题收掉：目录体积、缓存碎片、第一次使用的门槛。表名和列名没有变，已有的 SQL 可以直接跑。

## 引用表改为视图：同样的结果，一半的磁盘

一条调用指令会把 caller 的完整 descriptor（`Lcom/example/SomeVeryLongClassName;->method(Ljava/lang/String;)V`）抄进每一行。K-9 的 32.7 万条调用点里，这类重复文本有 104 MB，占目录的 55%。四张引用表加起来 224 MB 文本。

`method_refs` 早就是"同一批数据的另一种切法"，这次把同样的判断用到四张最热的表上：

| 公开关系 | 底层表 | 只存 |
| --- | --- | --- |
| `method_calls` | `_call_sites` | `file_id, class_idx, method_idx, callee_method_idx, callee` |
| `method_field_accesses` | `_field_accesses` | 索引 + `using_type` |
| `method_used_types` | `_used_types` | 索引 |
| `method_strings` | `_string_refs` | 索引 |

公开名字变成 join `methods` / `fields` / `types` / `strings` 的视图，列名、列序、行数都不变。`method_using_numbers` 也变成 `method_numbers` 的视图——它本来就是同一批行多带了三列 caller。

实测（同一台机器、同一轮，冷启动）：

| 查询 | 0.4.0 | 1.0.0 |
| --- | --- | --- |
| K-9 `method_calls` | 2.80 s / 454 MB / 182 MB | 2.66 s / 389 MB / 86 MB |
| VLC `method_calls` | 7.07 s / 1.22 GB / 398 MB | 7.06 s / 1.23 GB / 239 MB |

时间基本持平是刻意的结果：省下的写放大，正好被"查询调用关系时还要物化 methods 以完成 join"抵消。收益体现在磁盘上——对 11 MB 的 APK，缓存从 490 MB（两个按表集合分裂的目录）降到 86 MB。

按 callee 过滤仍然走索引：

```
SEARCH _call_sites USING INDEX idx_call_sites_callee (callee=?)
SEARCH methods USING COVERING INDEX idx_methods_key (file_id=? AND method_idx=?)
```

## 一个输入一份缓存

0.4.0 把表集合编进缓存键，所以"查 methods"和"查 calls"会留下两个几百 MB 的文件。现在键只含输入、档位与 `--debug`；缺表时引擎复制现有文件、补建缺的几张、原子替换。前一次查询付过的表不会被重写。

```
$ dexsql-tree query --input app.apk --cache --sql "SELECT count(*) FROM methods"     # 0.91 s，24 MB
$ dexsql-tree query --input app.apk --cache --sql "SELECT count(*) FROM method_calls" # 2.66 s，长出 86 MB
$ ls ~/.cache/dexsql-tree                                                             # 只有一个文件
```

配套两个小命令：

- 目录里写入了 `_meta.version`。`cache info` 汇总一个输入的全部目录与总大小。
- `cache prune` 删除不属于当前版本的目录。0.3.0 的键里带旧版本号，永远不会被命中，也不会自己消失——一个 11 MB 的包能留下 481 MB。

## 第一次使用

`start` 打印规模，然后问三个任何 APK 都值得问的问题，每条结果后面跟一行可复制的命令：

```
k9.apk  10.8 MB · 2 DEX · 64,586 methods · 11,635 classes · 69,933 strings

1. Which classes carry the most methods?
   class                                                    methods
   Lcom/google/android/material/textfield/TextInputLayout;  208
   ...
   next: dexsql-tree tables class_methods
```

它用惰性目录打开，只物化这三个问题读到的表，K-9 上 1.3 s / 320 MB。

报错也带建议：

```
error: SQL logic error: no such table: method_call (1)
did you mean "method_calls"? run `dexsql-tree tables` for the full list
```

最近邻匹配覆盖表和列；语法错误会提示检查字符串是否闭合。CLI、HTTP 与 MCP 共用这套文案。

`tables` 现在把内部表（`_` 开头）从清单里隐藏，四张引用表标为 `[view]`。

## 破坏性变更（都在可接受范围内）

1. `method_calls`、`method_field_accesses`、`method_used_types`、`method_strings`、`method_using_numbers` 从表变成视图。`SELECT`、`JOIN`、`WHERE` 写法不变；`PRAGMA table_info` 仍返回全部列，但 `sqlite_master.type` 变成 `view`。
2. `SELECT count(*) FROM method_calls` 现在要 join，K-9 0.42 s、VLC 0.95 s。只想知道规模时用 `v_overview`。
3. 缓存目录结构变了：旧版本的文件不会被复用。升级后跑一次 `dexsql-tree cache prune`。
4. `method_calls` 表里不再有 `method_class` 这种物理列，`WHERE method = ...` 会先匹配 `methods` 再取调用点，比按 callee 过滤慢一点。`tree` 和 `recipes callers` 走的是索引路径。

## 工程

- `scripts/build-all.sh` 现在会构建 Java 客户端与 GUI 并复制进 `dist/java/`，`sha256sums.txt` 覆盖 jar。之前这一步靠手工 `cp`，每次发布都要记得。
- CI 增加 `go test -race`（store/api/mcp）与 60 秒 `FuzzParseContainer`。fuzz 在第一次运行的几分钟内就撞出过一个按头部计数直接分配的 OOM，这类问题不该只靠本地偶尔跑一次。
- 索引按需：`idx_methods_key` 这类 join 索引只在对应视图要物化时才建，`SELECT count(*) FROM methods` 不会为不存在的 join 付 0.3 s。

## 验证

- `go test ./...`、`go vet`、`go test -race ./internal/store ./internal/api ./internal/mcp` 全部通过。
- 9 个目标交叉编译成功；`sha256sum -c sha256sums.txt` 覆盖 9 个二进制、2 个 jar 与两个演示输入。
- K-9 与 VLC 的冷启动、增量补表、命中三条路径都做了实测。
- ARM64 仍只做交叉编译验证；JavaFX 只做编译与 Java 测试，没有在物理显示器上回归。
