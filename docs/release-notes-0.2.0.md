# dexsql-tree 0.2.0 发布说明

开发者：**ByUsi Studio** · 许可：MIT

0.2.0 把"能用"推进到"顺手"：中文提示、免 SQL 的搜索与统计、内置查询模板、便捷视图，以及 9 个平台的交叉编译（含 ARM64 与 ARMv7）。引擎的解析与物化逻辑没有改动，0.1.0 的查询与实测数据全部仍然有效。

## 新增

### 1. 中文提示（`DEXSQL_TREE_ZH`）

设置 `DEXSQL_TREE_ZH=true` 后，帮助、报错、表格单位、调用树标题、统计标签全部切换为中文：

```bash
DEXSQL_TREE_ZH=true dexsql-tree help
dexsql-tree --lang zh stats --input app.apk      # 只对本次调用生效
```

- 接受 `true/1/yes/on/zh/zh-CN` 与 `false/0/no/off/en`。
- 兼容带连字符的 `DEXSQL-TREE_ZH`，也支持 `DEXSQL_TREE_LANG=zh|en`。
- **不读取系统 locale**，因此脚本输出可复现，CI 不会因环境不同而变语言。
- 结构化的 `--format json` 字段名与 SQL 列名保持英文，只有面向人的文本本地化。

### 2. `lang` 子命令：写入环境变量

```bash
dexsql-tree lang                  # 当前语言、来源、启用/关闭命令、启动文件路径
dexsql-tree lang set zh --write   # 写入 ~/.zshrc / ~/.bashrc / fish 配置
dexsql-tree lang set en --write
dexsql-tree lang unset --write    # 整块移除
dexsql-tree lang env              # 只输出赋值语句，可 eval 使用
```

- 自动识别 `$SHELL`，可用 `--shell bash|zsh|fish|powershell|cmd` 覆盖，`--file` 指定任意文件。
- 写入使用 `# >>> dexsql-tree language >>>` 标记块，重复执行不会重复追加。
- 各 shell 语法不同：`export` / `set -gx` / `$env:...` / `setx`。

### 3. 免 SQL 的三个入口

| 命令 | 作用 |
| --- | --- |
| `search --keyword X` | 跨类、方法、字段、字符串搜索；精确与前缀命中优先，字符串命中降权 |
| `stats` | 文件、DEX 字节、类/方法/字段/代码项/调用点、最大的类，一屏看完 |
| `recipes` / `query --recipe` | 8 个参数化查询模板，参数安全转义 |

`stats` 与 `search` 都直接读取已解析模型，不经过 SQLite，因此对大包也是秒级（K-9 11 MB：`stats` 0.46 s / 200 MB，`search` 0.47 s / 219 MB，单次扫描 49 ms），可用来在 `query` 之前判断是否值得物化。

### 4. 便捷视图，减少 JOIN

| 视图 | 用途 |
| --- | --- |
| `v_methods` | 方法 + `java_class` + `is_public`/`is_static`/`is_constructor` |
| `v_fields` | 字段 + `java_class` |
| `v_overview` | 每个关系的行数，一条 SQL 看清规模 |

```sql
SELECT java_class, name FROM v_methods WHERE name LIKE '%password%';
```

### 5. 内置查询模板

`callers`、`callees`、`methods-like`、`strings-like`、`api-usage`、`class-info`、`big-methods`、`urls`。CLI、HTTP、MCP 三端共用同一份 SQL，避免同一问题三种写法。

```bash
dexsql-tree query --input app.apk --recipe callers \
  --arg method='Lcom/example/Foo;->bar(I)V'
```

### 6. 更多平台：ARM64 与 ARMv7

`make release` 一次产出 9 个目标：

| 平台 | 架构 |
| --- | --- |
| Linux | amd64、arm64、armv7 |
| macOS | amd64、arm64 |
| Windows | amd64、arm64 |
| FreeBSD | amd64、arm64 |

引擎是纯 Go（`modernc.org/sqlite`），无需 cgo，因此 ARM64 服务器、树莓派 64 位系统、Apple Silicon、Windows on ARM 均为单文件静态二进制。发布包附带 `sha256sums.txt` 与演示 APK。

### 7. 协议层同步

- MCP 新增 `search`、`run_recipe`、`list_recipes` 三个工具。
- HTTP 新增 `GET /api/recipes`，`POST /api/query` 支持 `{recipe, args}`；`/api/health` 返回 `lang`。
- `/api/tables`、`list_tables` 现在同时返回视图与 `kind`。

## 变更

- 版本号 0.1.0 → 0.2.0（Go、Java 三个模块同步）。
- `query --limit` 默认值从"全部"改为 **200**；需要全量时显式写 `--limit 0`，输出会标注是否截断。
- `tables` 增加 `[view]` 标记；`method_refs."index"` 的引号约定不变。
- 新增 `internal/i18n`，`internal/store` 新增 `views.go`、`recipes.go`、`search.go`。

## 验证

- `go test ./...`：包含视图列名、搜索排序、模板转义与注入防护、语言变量优先级、语言开关解析的新测试。
- `go vet ./...` 通过。
- 9 个目标全部 `go build` 成功；`sha256sum -c sha256sums.txt` 全部 OK。
- Maven 测试包含真实启动 Go 二进制的 `EngineIntegrationTest`。

## 已知限制

与 0.1.0 相同：需要 SQL 的 `query`/`tree`/`serve`/`mcp` 仍要在打开时物化目录，49 MB 样本约 55 秒；写入单线程逐行插入；DEX 041 容器未拆分；`hiddenapi_flags` 未实现；JavaFX 端未做真机交互回归。`search` 与 `stats` 不物化，因此不受此限制。
