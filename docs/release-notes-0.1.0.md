# dexsql-tree 0.1.0 发布说明

开发者：**ByUsi Studio** · 许可：MIT

首个可运行版本，包含 Go 引擎、Java 客户端与 JavaFX 桌面端。发布前使用 F-Droid 上的真实 APK 做了 26 KB、684 KB、11 MB、22 MB、49 MB 五档验证，并修复了下面三个问题。

## 修复

### 1. `annotations_directory_item` 头部多读 4 字节

旧实现把该结构当成 5 个 `uint32`（误加了 `class_idx`），而规范是 16 字节：

```
class_annotations_off, fields_size, annotated_methods_size, annotated_parameters_size
```

错位后每条 field/method/parameter 记录都被解释成新的偏移，普通数据被当成注解集合。一个 684 KB 的 APK 因此产生 **8100 万个假元素**：

| 指标 | 修复前 | 修复后 |
| --- | ---: | ---: |
| 解析耗时 | 9.7 s | 0.038 s |
| 解析内存 | 586 MB | 14 MB |
| 完整打开 | 288 s / 4.4 GB | 1.03 s / 88 MB |
| 注解元素 | 81,188,026 | 1,306 |

新增 `internal/dex/annotations_test.go` 固定 16 字节布局，防止回归。

### 2. 行号表改为 `--debug` 按需开启

DEX 的 debug program 常常比代码本身更大。K-9（11 MB APK）有 **725 万**条 position，与 baksmali 输出的 646 万条 `.line`（首个 DEX）一致，属于真实数据而非解析错误。

默认只解析 `debug_info` 头部，不写入 `debug_positions`/`debug_locals`：

- K-9：51.8 s / 2.2 GB → 18.7 s / 1.45 GB
- 需要行号时加 `--debug`，数据完整（K-9 为 7,256,886 行）。

### 3. 新增 `--disk` 低内存模式

目录物化到系统临时文件，退出时自动删除：

- VLC（49 MB APK、38.6 MB DEX、6 个 DEX）：3.87 GB → 1.27 GB，耗时 55.7 s → 55.1 s
- K-9：1.45 GB → 549 MB

## 实测（Linux x64）

| 样本 | 体积 | DEX 字节 | 方法数 | 默认 | `--disk` |
| --- | ---: | ---: | ---: | --- | --- |
| `com.termux.boot` | 26 KB | 6 KB | 64 | 0.01 s / 12 MB | — |
| `getback_gps` | 684 KB | 825 KB | 7,281 | 1.03 s / 88 MB | — |
| `com.fsck.k9` | 11 MB | 12 MB | 68,095 | 18.7 s / 1.45 GB | 17.8 s / 549 MB |
| `pilipala` | 22 MB | 3.1 MB | 29,740 | 4.8 s / 294 MB | — |
| `org.videolan.vlc` | 49 MB | 38.6 MB | 270,289 | 55.7 s / 3.87 GB | 55.1 s / 1.27 GB |

12.7 KB 的 `search_to_browser` 没有 `classes.dex`，会明确报 `no readable dex entry`，而不是静默产生空库。

## 已知限制

- 物化模型下，内存随调用点/字符串引用线性增长；几十 MB 的 DEX 建议 `--disk`。
- 单线程逐行写入；批量 INSERT 在纯 Go SQLite 驱动上反而更慢，已回退。
- DEX 041 容器未拆分，`hiddenapi_flags` 未实现。
- 大样本使用开源 APK，未覆盖商业加固/混淆包。
