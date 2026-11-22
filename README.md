# dexsql-tree

用 SQL 查询 Android APK / DEX，并把调用关系展开成树。**Go 引擎 + Java 客户端**，MIT 许可，完全开源。

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
| `query --input X --sql "..."` | 执行一条只读 SQL；`--limit`、`--format table\|json\|csv\|md`、`--output` |
| `query --input X` | 终端交互式 REPL（`.tables`、`.schema methods`、`.quit`） |
| `tables [--json]` | 列出 55 个关系及其说明，不需要输入文件 |
| `tree --input X --method D` | 调用树；`--direction callers\|callees`、`--depth`、`--format text\|json\|dot` |
| `smali --input X --method D` | 反汇编单个方法 |
| `serve --input X [--addr ...]` | HTTP API + Web UI |
| `mcp [--input X]` | stdio MCP 服务器 |

`--input` 可以重复，多个 APK/DEX 会被合并到一个 `file_id` 空间：

```bash
./bin/dexsql-tree query --input base.apk --input split_config.dex \
  --sql "SELECT file_id, count(*) FROM classes GROUP BY file_id"
```

### 常用查询

```sql
-- 1. 谁引用了这个 URL
SELECT method, value FROM method_strings
WHERE value LIKE '%api.example.com%';

-- 2. 这个方法的直接调用者（按调用点计数）
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
| `call_tree` | 调用者/被调用者树 |
| `smali` | 单方法反汇编 |

协议为 JSON-RPC 2.0、按行分隔，支持 `initialize`、`tools/list`、`tools/call`、`ping`。

---

## 6. HTTP API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | `{ok, version}` |
| GET | `/api/datasets` | 已加载数据集与统计 |
| POST | `/api/datasets` | `{path}`，解析并注册 |
| GET | `/api/tables` | 55 个关系及列 |
| GET | `/api/table?name=methods` | 单个关系定义 |
| POST | `/api/query` | `{sql, limit, dataset}` |
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
│   ├── dexsql-tree/       CLI 入口（query/tables/tree/smali/serve/mcp）
│   └── dexgen/            生成演示 DEX/APK
├── internal/
│   ├── dex/               DEX 解析、指令解码、debug_info、注解、smali、APK 读取
│   ├── store/             SQLite 物化、55 张表、只读守卫
│   ├── tree/              调用图、树构建、环检测、DOT 输出
│   ├── api/               HTTP API 与内置 Playground
│   ├── mcp/               stdio MCP 服务器
│   └── dextest/           合成 DEX 构造器（测试与演示共用）
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
- 55 张关系表，覆盖实体、引用、字节码、注解、调试信息与 DEX 结构。
- 调用图（按调用点计数）与带环检测的树视图。
- 只读保护：`PRAGMA query_only=ON` + 语句白名单。

**当前限制（请按需评估）**

1. **物化模型**：所有表在打开时写入内存 SQLite，因此大 APK（数百 MB、50+ DEX）会有明显的内存占用与数秒级打开时间。DexSQL 采用列式与惰性准备，这是它更快的原因。这里优先选择"实现简单、SQL 完整、可审计"。
2. **未做 041 容器拆分**：DEX 041 的多逻辑 DEX 会被当成一个文件；`dex_files` 不会合成 `classes.dex#2`。
3. **`hiddenapi_flags` 未实现**：该节位于 DEX 尾部，当前版本未解析。
4. **性能未针对大样本调优**：没有批量插入优化、并行解析或缓存文件（DexSQL 的 `cache.dxc2`）。
5. **尚未在大型商业 APK 上做基准**：验证样本是合成 DEX 与单元测试。请先用目标 APK 跑 `tables`/`query` 验证解析覆盖率。
6. **JavaFX 桌面端未在真实显示器上做交互测试**：只验证了编译与客户端↔引擎的端到端调用。
7. SQLite 关键字冲突：`method_refs."index"` 需要引号。

**路线图**

- 并行解析多 DEX + 批量插入，降低大包打开时间
- 持久化缓存（`.dxsql`），支持 `query-cache`
- 041 容器拆分与 `hiddenapi_flags`
- 覆盖方法/字段的 override 关系展开（当前只按字节码直接引用）
- JavaFX 端虚拟化大结果集、SQL 语法高亮

---

## 9. 开发

```bash
make test          # go test ./... + mvn test
make build         # Go 二进制 + Java 包
make demo          # 生成 testdata 并跑一组演示查询
make fmt           # gofmt
```

CI 配置见 `.github/workflows/ci.yml`（Go 1.21 + JDK 20）。

## 10. 许可

MIT。见 [LICENSE](LICENSE)。

DEX 格式与 SQL 表命名参考了 Android 官方文档与 DexSQL 的公开文档（MIT）；未使用 DexSQL 的二进制或源代码。
