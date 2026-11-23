# dexsql-tree

用 SQL 查询 Android APK / DEX，并把调用关系展开成树。**Go 引擎 + Java 客户端**，MIT 许可，完全开源。

开发者：**ByUsi Studio**。

> 目标不是复刻一个二进制，而是复刻一种工作方式：把 `classes.dex` 里的类、方法、字符串、字段访问、调用点当作关系表，让分析者用一条 SQL 回答"谁引用了这个字符串、谁调用了它、它又调用了谁"。

```bash
dexsql-tree query --input app.apk \
  --sql "SELECT method, value FROM method_strings WHERE value LIKE '%api.example.com%'"

dexsql-tree tree --input app.apk \
  --method 'Lcom/example/Auth;->login(Ljava/lang/String;)V' --direction callers --depth 3
```

---

## 1. 为什么会有这个项目

LSPosed 的 [DexSQL](https://github.com/LSPosed/dexsql-release) 证明了"DEX 当数据库查"是非常高效的分析范式，但它本身是**闭源软件**，只以二进制形式发布，使用 [DexSQL Binary License 1.0](https://github.com/LSPosed/dexsql-release/blob/master/LICENSE)：可以免费使用和再分发二进制，但拿不到源码，也无法审计解析逻辑、扩展表结构或嵌入自己的产品。

`dexsql-tree` 是一个独立的、clean-room 的开源实现：

| | DexSQL（LSPosed） | dexsql-tree |
| --- | --- | --- |
| 源码 | 闭源 | 全部开源（MIT） |
| 解析层 | 原生实现，自描述为直接读 DEX、复用已加载数据集 | Go 实现，按 DEX 规范逐节解码 |
| 查询层 | 内置只读 SQL 子集 + 列式存储 | SQLite（纯 Go 驱动 `modernc.org/sqlite`），完整 SQL |
| 分发 | 单文件二进制 + MCP | 单文件二进制 + MCP + HTTP API + JavaFX 桌面端 |
| 调用链 | 通过 SQL 自连接 | 额外提供 `tree` 子命令和 `/api/tree`，带环检测与深度限制 |
| 许可 | Binary License（禁止获取源码） | MIT |

**没有复制 DexSQL 的任何代码或资源。** 表名与列名对齐其公开文档（文档本身是 MIT），目的是降低迁移成本；实现、数据结构与查询方式完全不同。

更详细的原项目拆解见 [docs/analysis-dexsql.md](docs/analysis-dexsql.md)。

---

## 2. 架构

```mermaid
flowchart LR
    subgraph Go["Go 引擎（单二进制）"]
        A["APK / DEX / JAR"] --> B["internal/dex<br/>DEX 解析 + 指令解码 + smali"]
        B --> C["internal/store<br/>55 张关系表 / SQLite 物化"]
        C --> D["CLI<br/>query · tables · smali"]
        C --> E["HTTP API<br/>/api/query · /api/tree"]
        B --> F["internal/tree<br/>调用图 + 环检测"]
        F --> E
        E --> G["stdio MCP"]
    end
    E --> H["Java 客户端<br/>dexsql-tree-client"]
    H --> I["JavaFX 桌面端<br/>dexsql-tree-gui"]
    E --> J["内置 Web Playground"]
```

职责划分：

- **Go**：所有二进制解析、SQL 物化、调用图、协议适配。无 CGO，交叉编译简单。
- **Java**：只做展示与集成。`dexsql-tree-client` 是一个零依赖（除 Jackson）的 HTTP 客户端；`dexsql-tree-gui` 是 JavaFX 桌面端。
- 两个语言共用同一个 HTTP 契约，保证 CLI、MCP、GUI 的查询结果完全一致。

详细设计见 [docs/architecture.md](docs/architecture.md)。

---

## 3. 快速开始

### 3.1 构建 Go 引擎

需要 Go 1.21+。

```bash
git clone <this-repo> dexsql-tree
cd dexsql-tree
go build -o bin/dexsql-tree ./cmd/dexsql-tree

# 生成一个 672 字节的演示 DEX/APK（源码在 internal/dextest）
go run ./cmd/dexgen -out testdata
```

验证：

```bash
./bin/dexsql-tree query --input testdata/demo.apk \
  --sql "SELECT descriptor, access_flags FROM methods"
```

输出：

```
descriptor                                           access_flags
---------------------------------------------------  ------------
Ldemo/Hello;->main([Ljava/lang/String;)V             9
Ljava/io/PrintStream;->println(Ljava/lang/String;)V  NULL
```

`NULL` 的含义是这个 DEX 只**引用**了该方法，没有定义它。这也是 DexSQL 数据模型里"引用 vs 定义"的核心区分。

### 支持的平台

引擎不依赖 cgo（SQLite 使用纯 Go 实现），所以全部平台可由一条命令交叉编译：

```bash
make release          # 产物写入 dist/dexsql-tree-<版本>/
```

| 平台 | 架构 | 目录 |
| --- | --- | --- |
| Linux | x86-64 / ARM64 / ARMv7 | `linux-amd64`、`linux-arm64`、`linux-armv7` |
| macOS | Intel / Apple Silicon | `darwin-amd64`、`darwin-arm64` |
| Windows | x64 / ARM64 | `windows-amd64`、`windows-arm64` |
| FreeBSD | x86-64 / ARM64 | `freebsd-amd64`、`freebsd-arm64` |

ARM64 覆盖服务器（Ampere/Graviton）、树莓派 4/5 的 64 位系统、Apple Silicon 与 Windows on ARM；ARMv7 供树莓派 2/3 等 32 位设备使用。脚本同时生成 `sha256sums.txt` 与演示 APK，便于解压即测。

### 3.2 构建 Java 客户端与桌面端

需要 JDK 20+ 与 Maven 3.9+。

```bash
cd java
mvn install                    # 编译 client 与 gui、运行测试并安装到本地仓库
mvn -pl dexsql-tree-gui javafx:run \
  -Ddexsql.tree.bin=../bin/dexsql-tree \
  -Ddexsql.args="--input ../testdata/demo.apk"
```

桌面端也可以连接已经在运行的引擎：

```bash
./bin/dexsql-tree serve --input app.apk --addr 127.0.0.1:8765
mvn -pl dexsql-tree-gui javafx:run -Ddexsql.args="--connect http://127.0.0.1:8765"
```

### 3.3 内置 Web Playground

```bash
./bin/dexsql-tree serve --input app.apk
# 打开 http://127.0.0.1:8765
```

页面提供表目录、SQL 编辑器（Ctrl+Enter 执行）和调用树视图，不依赖任何 CDN。

![SQL playground](docs/images/playground-sql.png)

![Call tree](docs/images/playground-tree.png)

---

## 4. CLI

| 命令 | 用途 |
| --- | --- |
| `query --input X --sql "..."` | 执行一条只读 SQL；`--limit`（默认 200，`0` 为全部）、`--format table\|json\|csv\|md`、`--output` |
| `query --input X --recipe callers --arg method=D` | 跑内置查询模板，不用写 SQL |
| `query --input X` | 终端交互式 REPL（`.tables`、`.schema methods`、`.quit`） |
| `search --input X --keyword 关键词` | 一次搜类、方法、字段、字符串；`--kind` 可限定范围；直接扫模型，不建 SQLite |
| `stats --input X [--json]` | 一屏总览：文件、类、方法、调用点、最大的类 |
| `recipes [名称]` | 列出内置模板，给名称时打印 SQL 与参数 |
| `tables [--json]` | 列出 55 张表与 3 个便捷视图 |
| `tree --input X --method D` | 调用树；`--direction callers\|callees`、`--depth`、`--format text\|json\|dot` |
| `smali --input X --method D` | 反汇编单个方法 |
| `serve --input X [--addr ...]` | HTTP API + Web UI |
| `mcp [--input X]` | stdio MCP 服务器 |
| `lang set zh --write` | 把中文提示写入 shell 启动文件 |

### 中文提示

CLI 的所有提示、帮助、报错和表格单位都支持中英双语。切换方式有三种，优先级从高到低：

```bash
DEXSQL_TREE_ZH=true dexsql-tree stats --input app.apk   # 本次调用
dexsql-tree --lang zh tables                            # 本次调用（命令行开关）
dexsql-tree lang set zh --write                         # 永久写入 ~/.zshrc 等启动文件
```

- 变量名是 `DEXSQL_TREE_ZH`，值为 `true`/`1`/`yes`/`on`/`zh` 时启用中文；`false`/`0`/`en` 保持英文。
- 同时接受带连字符的 `DEXSQL-TREE_ZH`（部分 `.env` 工具会保留连字符）以及 `DEXSQL_TREE_LANG=zh|en`。
- 语言只由变量或开关决定，不读取系统 locale，脚本输出因此可复现。

`lang` 子命令：

| 命令 | 行为 |
| --- | --- |
| `lang` / `lang show` | 显示当前语言、来源变量、启用/关闭命令、启动文件路径 |
| `lang set zh` / `lang set en` | 打印对应 shell 的赋值语句；加 `--write` 写入启动文件 |
| `lang unset` | 打印清除变量的语句；加 `--write` 删除写入过的配置块 |
| `lang env` | 只输出赋值语句，便于 `eval "$(dexsql-tree lang env)"` |
| `lang path` | 打印将要修改的启动文件 |

`--shell bash|zsh|fish|powershell|cmd` 可覆盖自动检测；`--file 路径` 可指定任意文件。写入使用带标记的配置块，重复执行不会重复追加，`unset --write` 会整块移除。

### 简化查询：视图与模板

大多数问题不需要写 JOIN。打开数据后有三层递进的捷径：

```sql
-- 1) 视图：方法已带 Java 类名和常用访问标志
SELECT java_class, name, opcode_count FROM v_methods WHERE name LIKE '%password%';

-- 2) 概览视图：一条语句看清各表规模
SELECT * FROM v_overview;
```

```bash
# 3) 模板：参数化查询，CLI / HTTP / MCP 共用同一份 SQL
dexsql-tree query --input app.apk --recipe callers \
  --arg method='Lcom/example/Foo;->bar(I)V'

dexsql-tree recipes callers       # 查看参数与展开后的 SQL
dexsql-tree recipes               # 全部模板
```

内置模板：`callers`、`callees`、`methods-like`、`strings-like`、`api-usage`、`class-info`、`big-methods`、`urls`。参数会被安全转义，注入的多语句会被只读守卫拒绝。

两个开关控制"打开时做什么"：

| 开关 | 作用 | 何时使用 |
| --- | --- | --- |
| `--disk` | 把目录物化到系统临时文件，退出时删除 | 大 APK 或内存紧张；内存通常降到 1/2–1/3，耗时基本不变 |
| `--debug` | 额外写入 `debug_positions` / `debug_locals` | 需要行号、局部变量；这些表往往比代码本身大，默认关闭 |

`--input` 可以重复，多个 APK/DEX 会被合并到一个 `file_id` 空间：

```bash
./bin/dexsql-tree query --input base.apk --input split_config.dex \
  --sql "SELECT file_id, count(*) FROM classes GROUP BY file_id"
```

### 常用查询

```sql
-- 1. 谁引用了这个 URL（也可以直接 search --keyword api.example.com）
SELECT method, value FROM method_strings
WHERE value LIKE '%api.example.com%';

-- 2. 这个方法的直接调用者（等价于 --recipe callers）
SELECT method, count(*) AS sites FROM method_calls
WHERE callee = 'Lcom/example/Foo;->bar(I)V'
GROUP BY method ORDER BY sites DESC;

-- 3. 两跳调用者
SELECT c1.method AS caller, c2.method AS caller_of_caller
FROM method_calls c1
JOIN method_calls c2 ON c2.callee = c1.method
WHERE c1.callee = 'Lcom/example/Foo;->bar(I)V';

-- 4. 字符串 + 调用关系联合过滤
SELECT DISTINCT c.method
FROM method_strings s
JOIN method_calls c ON c.method = s.method
WHERE s.value LIKE '%token%'
  AND c.callee LIKE '%HttpURLConnection;->connect%';

-- 5. 某个类自己声明的方法与访问标志
SELECT method_name, kind, access_flags
FROM class_methods WHERE class_descriptor = 'Lcom/example/Foo;';

-- 6. 带注解的类
SELECT owner, annotation, element_name, value_i64
FROM annotation_elements_resolved
WHERE owner_kind = 'class' AND annotation = 'Lkotlin/Metadata;';
```

完整关系清单：[docs/schema.md](docs/schema.md)，更多语句见 [examples/queries.sql](examples/queries.sql)。也可以随时运行 `dexsql-tree tables` 查看二进制内置的目录。

> 注意：`method_refs` 有一列名为 `index`，它是 SQLite 关键字，查询时必须写成 `"index"`。

### 调用树

```bash
./bin/dexsql-tree tree --input app.apk \
  --method 'Ljava/io/PrintStream;->println(Ljava/lang/String;)V' \
  --direction callers --depth 2
```

```
callers of Ljava/io/PrintStream;->println(Ljava/lang/String;)V (depth 2, 2 nodes)
Ljava/io/PrintStream;->println(Ljava/lang/String;)V
└─ Ldemo/Hello;->main([Ljava/lang/String;)V
```

`--format dot` 可直接喂给 Graphviz；`--format json` 供程序消费。

---

## 5. MCP

```json
{
  "mcpServers": {
    "dexsql-tree": {
      "command": "/absolute/path/to/dexsql-tree",
      "args": ["mcp", "--input", "/absolute/path/to/app.apk"]
    }
  }
}
```

工具：

| 工具 | 作用 |
| --- | --- |
| `open_dataset` | 打开 APK/DEX，返回 dataset id 与统计 |
| `list_datasets` | 当前会话的数据集 |
| `list_tables` / `describe_table` | 表目录与列定义 |
| `query` | 只读 SQL，默认最多 200 行 |
| `search` | 关键词搜索类/方法/字段/字符串，免写 SQL |
| `run_recipe` / `list_recipes` | 运行内置参数化查询 |
| `call_tree` | 调用者/被调用者树 |
| `smali` | 单方法反汇编 |

协议为 JSON-RPC 2.0、按行分隔，支持 `initialize`、`tools/list`、`tools/call`、`ping`。

---

## 6. HTTP API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | `{ok, version, lang}` |
| GET | `/api/datasets` | 已加载数据集与统计 |
| POST | `/api/datasets` | `{path}`，解析并注册 |
| GET | `/api/tables` | 表与视图及列 |
| GET | `/api/table?name=methods` | 单个关系定义 |
| GET | `/api/recipes[?name=callers]` | 内置模板目录或单个模板 |
| POST | `/api/query` | `{sql, limit, dataset}` 或 `{recipe, args, limit, dataset}` |
| POST | `/api/tree` | `{method, direction, depth, maxNodes, dataset}` |
| GET | `/api/smali?method=...` | 反汇编文本 |

所有接口允许跨域，`dataset` 省略时使用唯一已加载的数据集。服务默认只监听 `127.0.0.1`，没有鉴权；不要把 `serve` 直接暴露到公网。

Java 调用示例：

```java
try (DexsqlProcess engine = DexsqlProcess.start(Path.of("bin/dexsql-tree"), Path.of("app.apk"))) {
    DexsqlClient client = engine.client();
    QueryResult r = client.query("SELECT descriptor FROM classes LIMIT 10");
    for (List<Object> row : r.rows()) {
        System.out.println(row.get(0));
    }
    CallTree tree = client.tree("Lcom/example/Foo;->bar(I)V", Direction.CALLERS, 3);
}
```

---

## 7. 目录结构

```
dexsql-tree/
├── cmd/
│   ├── dexsql-tree/       CLI 入口（query/search/stats/recipes/tables/tree/smali/serve/mcp/lang）
│   └── dexgen/            生成演示 DEX/APK
├── internal/
│   ├── dex/               DEX 解析、指令解码、debug_info、注解、smali、APK 读取
│   ├── store/             SQLite 物化、55 张表、视图、查询模板、只读守卫
│   ├── tree/              调用图、树构建、环检测、DOT 输出
│   ├── api/               HTTP API 与内置 Playground
│   ├── mcp/               stdio MCP 服务器
│   ├── i18n/              中英文案与环境变量/开关解析
│   └── dextest/           合成 DEX 构造器（测试与演示共用）
├── scripts/
│   └── build-all.sh       9 个平台目标的交叉编译 + 校验和
├── java/
│   ├── dexsql-tree-client/ HTTP 客户端、模型、进程管理、集成测试
│   └── dexsql-tree-gui/    JavaFX 桌面端
├── docs/                  原项目分析、架构、SQL 参考、Java 指南
└── testdata/              demo.dex / demo.apk（由 dexgen 生成）
```

---

## 8. 实现范围与已知限制

**已实现**

- DEX 035–039 常用节：字符串池、type/proto/field/method id、class_def、class_data、code_item、try/catch、debug_info、注解目录与编码值、method_handle、call_site、map_list、encoded_array。
- MUTF-8 解码（含 U+0000 与代理对）、Java 风格类型名、Dalvik 指令长度与引用提取、常量提取、smali 文本。
- 55 张关系表，覆盖实体、引用、字节码、注解、调试信息与 DEX 结构；另有 `v_methods`、`v_fields`、`v_overview` 三个便捷视图。
- 8 个内置查询模板（调用者、被调用者、API 使用量、最大方法、疑似凭据字符串等），CLI/HTTP/MCP 共用。
- 调用图（按调用点计数）与带环检测的树视图。
- 中英双语 CLI（`DEXSQL_TREE_ZH`）、关键词搜索与一屏统计。
- 只读保护：`PRAGMA query_only=ON` + 语句白名单。
- 9 个交叉编译目标，覆盖 amd64/arm64/armv7。

**真实 APK 实测（Linux x64，Go 1.21，内存/磁盘两种模式）**

| 样本 | 体积 | DEX 字节 | 方法数 | 调用点 | 默认（内存） | `--disk` |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| F-Droid `com.termux.boot` | 26 KB | 6 KB | 64 | 0 | 0.01 s / 12 MB | — |
| F-Droid `getback_gps` | 684 KB | 825 KB | 7,281 | 26,520 | 1.03 s / 88 MB | — |
| F-Droid `com.fsck.k9` | 11 MB | 12 MB | 68,095 | 348,191 | 18.7 s / 1.45 GB | 17.8 s / 549 MB |
| F-Droid `pilipala` | 22 MB | 3.1 MB | 29,740 | 80,355 | 4.8 s / 294 MB | — |
| F-Droid `org.videolan.vlc` | 49 MB | 38.6 MB | 270,289 | 736,471 | 55.7 s / 3.87 GB | 55.1 s / 1.27 GB |

行号表（`--debug`）是单独的开销：`k9` 有 725 万条 position，开启后耗时约 50 s、磁盘模式内存约 0.94 GB。默认关闭，只解析 `debug_info` 头部。

**当前限制（请按需评估）**

1. **物化模型**：所有表在打开时写入 SQLite（默认内存，`--disk` 为临时文件）。数据本身随调用点/字符串引用线性增长，几十 MB 的 DEX 需要 GB 级内存或 `--disk`。DexSQL 采用列式与惰性准备，这是它更快的原因；这里优先选择"实现简单、SQL 完整、可审计"。
2. **未做 041 容器拆分**：DEX 041 的多逻辑 DEX 会被当成一个文件；`dex_files` 不会合成 `classes.dex#2`。
3. **`hiddenapi_flags` 未实现**：该节位于 DEX 尾部，当前版本未解析。
4. **写入是单线程逐行插入**：批量多行 INSERT 在纯 Go SQLite 驱动上反而更慢（语句重新编译），已回退；并行解析多 DEX 尚未实现。
5. **大样本以开源 APK 验证**：已覆盖 6 个 DEX、38.6 MB 的 VLC，但未做商业加固/混淆包的基准。
6. **JavaFX 桌面端未在真实显示器上做交互测试**：只验证了编译与客户端↔引擎的端到端调用。
7. SQLite 关键字冲突：`method_refs."index"` 需要引号。

**路线图**

- 并行解析多 DEX + 更快的写入路径，降低大包打开时间
- 模板与视图继续扩充（override 关系、xref 汇总）
- 持久化缓存（`.dxsql`），支持 `query-cache`
- 041 容器拆分与 `hiddenapi_flags`
- 覆盖方法/字段的 override 关系展开（当前只按字节码直接引用）
- JavaFX 端虚拟化大结果集、SQL 语法高亮

---

## 9. 开发

```bash
make test          # go test ./... + mvn test
make build         # Go 二进制（带版本号）
make release       # 交叉编译 9 个平台到 dist/
make cross         # 快速验证 ARM64 目标可编译
make demo          # 生成 testdata 并跑一组演示查询
make fmt           # gofmt
```

CI 配置见 `.github/workflows/ci.yml`：一个 job 跑测试，一个矩阵 job 编译 9 个平台目标，一个 job 跑 Maven（含真实启动 Go 二进制的集成测试）。

## 10. 许可

MIT，Copyright (c) 2026 **ByUsi Studio**。见 [LICENSE](LICENSE)。

DEX 格式与 SQL 表命名参考了 Android 官方文档与 DexSQL 的公开文档（MIT）；未使用 DexSQL 的二进制或源代码。
