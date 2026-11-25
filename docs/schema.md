# SQL 关系参考

本文件由 `dexsql-tree tables --json` 生成，与二进制内置目录一致。列类型为逻辑类型：`UInt32`/`Int64` 映射 SQLite INTEGER，`Float64` 映射 REAL，`Utf8`/`Utf8View` 映射 TEXT，`Boolean` 映射 0/1。

四条使用约定：

- **表存在不等于有数据**。0.4.0 起只物化语句读到的表；未物化的表结构仍在，但查询它会明确报错（CLI 提示加 `--all-tables`），而不是返回 0 行。`fast` 档另外默认跳过 `method_refs` 与 `id_items` 两张派生表；语句点名它们时会按需构建。
- **五张引用关系是视图**。`method_calls`、`method_field_accesses`、`method_used_types`、`method_strings`、`method_using_numbers` 的列与之前完全相同，但底层只存整数索引（`_call_sites`、`_field_accesses`、`_used_types`、`_string_refs`），再 join `methods`/`fields`/`types`/`strings` 还原名字。按 callee 过滤仍走索引；`count(*)` 会真的 join，只想知道规模时用 `v_overview`。
- `debug_positions` 与 `debug_locals` 默认是空表。它们由 `--debug` 开启，并且只有语句引用调试表时才写入；大型 DEX 的行号程序常常比代码本身更大（例如 11 MB 的 K-9 有 725 万条 position）。
- `method_refs."index"` 是 SQLite 关键字，查询时必须写成 `"index"` 或 `method_refs.[index]`。

## 一览

| 关系 | 列数 | 说明 |
| --- | ---: | --- |
| `dex_files` | 28 | One row per logical DEX loaded from the input. |
| `strings` | 3 | DEX string pool entries; the key is (string_pool_id, string_idx). |
| `type_ids` | 3 | Raw type_ids section (descriptor string index only). |
| `types` | 4 | One row per referenced type, with descriptor and Java-style name. |
| `proto_ids` | 5 | Raw proto_id_item rows. |
| `protos` | 9 | Method prototypes with a joined parameter descriptor list. |
| `proto_params` | 5 | One row per prototype parameter. |
| `field_ids` | 5 | Raw field_ids section. |
| `fields` | 8 | Referenced fields with resolved names and full descriptors. |
| `method_ids` | 5 | Raw method_ids section. |
| `methods` | 14 | All referenced methods; access/opcode columns are NULL for reference-only rows. |
| `method_opcodes` | 4 | Per-method opcode summary. |
| `class_defs` | 10 | Raw class_def_item records (indices and offsets only). |
| `classes` | 8 | Class definitions with resolved descriptors. |
| `class_data_items` | 8 | Summary of each class_data_item. |
| `class_data_fields` | 5 | Field definitions from class_data, with kind and flags. |
| `class_data_methods` | 5 | Method definitions from class_data, with kind and flags. |
| `class_methods` | 8 | Methods a class itself declares. |
| `class_fields` | 8 | Fields a class itself declares. |
| `class_interfaces` | 5 | Interfaces directly declared by a class. |
| `type_lists` | 4 | Raw type_list structures. |
| `type_list_entries` | 4 | Per-entry expansion of each type_list. |
| `id_items` | 6 | Uniform raw offsets of the id sections. |
| `map_items` | 5 | The DEX map_list. |
| `method_refs` | 7 | Sparse per-instruction reference table over bytecode. |
| `method_numbers` | 7 | Immediate numeric constants loaded by bytecode. |
| `method_strings` | 8 | String constants referenced by each method (one row per instruction). *view* |
| `method_calls` | 10 | Call instructions: caller columns plus callee columns. *view* |
| `method_field_accesses` | 12 | Field read/write instructions. *view* |
| `method_used_types` | 9 | Type-referencing instructions (check-cast, new-instance, ...). *view* |
| `method_using_numbers` | 10 | Immediate numeric constants with the using method resolved. *view* |
| `code_items` | 12 | code_item header and instruction layout. |
| `try_items` | 6 | Protected ranges inside a code item. |
| `catch_handlers` | 8 | Typed catch and catch-all entries. |
| `debug_info` | 5 | Method debug_info_item summary. |
| `debug_parameters` | 5 | Parameter names from the debug header. |
| `debug_positions` | 9 | Source positions from the debug state machine. |
| `debug_locals` | 10 | Local-variable events from the debug state machine. |
| `annotations` | 5 | One row per annotation_item. |
| `annotation_elements` | 11 | Encoded-value tree of every annotation item. |
| `annotation_directories` | 7 | Summary of class annotation directories. |
| `annotation_directory_entries` | 7 | Field/method/parameter entries of annotation directories. |
| `annotation_sets` | 3 | Raw annotation_set_item summaries. |
| `annotation_set_entries` | 4 | Per-annotation expansion of annotation sets. |
| `annotation_set_ref_lists` | 3 | Parameter annotation set ref lists. |
| `annotation_set_refs` | 4 | Per-parameter-position entries of a ref list. |
| `annotation_elements_resolved` | 17 | Annotation elements joined to their owners. |
| `annotation_values` | 16 | Owner-resolved annotation value tree at every depth. |
| `encoded_arrays` | 5 | All discovered encoded_array_items. |
| `encoded_array_values` | 11 | Every value node inside an encoded array. |
| `encoded_array_owners` | 4 | Links encoded arrays to their owners. |
| `class_static_values` | 9 | Static field initial values in declaration order. |
| `call_sites` | 7 | DEX 038+ call_site_id_item summaries. |
| `call_site_args` | 8 | Top-level encoded values of a call site. |
| `method_handles` | 4 | DEX 038+ method_handle_item rows. |
| `hiddenapi_flags` | 7 | Hidden API restriction per field/method (map_list type 0xF000). |

### 便捷视图

视图不是新数据，只是把常用的 JOIN 预先写好。它们在打开数据时一次性创建，查询时才按需计算，因此不用时没有成本。

| 视图 | 列数 | 说明 |
| --- | ---: | --- |
| `v_methods` | 14 | `methods` 左连接 `types`，补上 `java_class` 与 `is_public`/`is_static`/`is_constructor` |
| `v_fields` | 7 | `fields` 左连接 `types`，补上 `java_class` |
| `v_overview` | 2 | 每个关系一行，`row_count` 为行数，用于一眼看清输入规模 |

```sql
-- 不用自己写 methods JOIN types
SELECT java_class, name, param_count
FROM v_methods
WHERE name = 'onCreate' AND is_public;

-- 判断这个样本值不值得细查
SELECT * FROM v_overview;
```

`java_class` 为 `demo.Hello` 这样的点分名；`class_descriptor` 仍是 `Ldemo/Hello;`，两者都在视图中。

另外五张引用关系（`method_calls` 等）也是视图，但它们的列与历史上的表完全一致，因此列在下方"列定义"里而不是这里。

## 列定义

### `dex_files`

One row per logical DEX loaded from the input.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `string_pool_id` | UInt32 |
| `source_path` | Utf8 |
| `entry_name` | Utf8 |
| `byte_len` | UInt64 |
| `dex_version` | Utf8 |
| `checksum` | UInt32 |
| `signature_hex` | Utf8 |
| `file_size` | UInt32 |
| `header_size` | UInt32 |
| `endian_tag` | UInt32 |
| `link_size` | UInt32 |
| `link_off` | UInt32 |
| `map_off` | UInt32 |
| `string_count` | UInt32 |
| `string_ids_off` | UInt32 |
| `type_count` | UInt32 |
| `type_ids_off` | UInt32 |
| `proto_count` | UInt32 |
| `proto_ids_off` | UInt32 |
| `field_count` | UInt32 |
| `field_ids_off` | UInt32 |
| `method_count` | UInt32 |
| `method_ids_off` | UInt32 |
| `class_count` | UInt32 |
| `class_defs_off` | UInt32 |
| `data_size` | UInt32 |
| `data_off` | UInt32 |

### `strings`

DEX string pool entries; the key is (string_pool_id, string_idx).

| 列 | 类型 |
| --- | --- |
| `string_pool_id` | UInt32 |
| `string_idx` | UInt32 |
| `value` | Utf8View |

### `type_ids`

Raw type_ids section (descriptor string index only).

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `type_idx` | UInt32 |
| `descriptor_idx` | UInt32 |

### `types`

One row per referenced type, with descriptor and Java-style name.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `type_idx` | UInt32 |
| `descriptor` | Utf8 |
| `java_name` | Utf8 |

### `proto_ids`

Raw proto_id_item rows.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `proto_idx` | UInt32 |
| `shorty_idx` | UInt32 |
| `return_type_idx` | UInt32 |
| `parameters_off` | UInt32 |

### `protos`

Method prototypes with a joined parameter descriptor list.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `proto_idx` | UInt32 |
| `shorty_idx` | UInt32 |
| `shorty` | Utf8 |
| `return_type_idx` | UInt32 |
| `return_descriptor` | Utf8 |
| `parameters_off` | UInt32 |
| `param_count` | UInt32 |
| `param_descriptors` | Utf8 |

### `proto_params`

One row per prototype parameter.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `proto_idx` | UInt32 |
| `ordinal` | UInt32 |
| `type_idx` | UInt32 |
| `descriptor` | Utf8 |

### `field_ids`

Raw field_ids section.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `field_idx` | UInt32 |
| `class_idx` | UInt32 |
| `type_idx` | UInt32 |
| `name_idx` | UInt32 |

### `fields`

Referenced fields with resolved names and full descriptors.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `field_idx` | UInt32 |
| `class_idx` | UInt32 |
| `class_descriptor` | Utf8 |
| `type_idx` | UInt32 |
| `type_descriptor` | Utf8 |
| `name` | Utf8 |
| `descriptor` | Utf8 |

### `method_ids`

Raw method_ids section.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `class_idx` | UInt32 |
| `proto_idx` | UInt32 |
| `name_idx` | UInt32 |

### `methods`

All referenced methods; access/opcode columns are NULL for reference-only rows.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `class_idx` | UInt32 |
| `class_descriptor` | Utf8View |
| `proto_idx` | UInt32 |
| `name` | Utf8View |
| `descriptor` | Utf8View |
| `return_descriptor` | Utf8View |
| `param_count` | UInt32 |
| `param_descriptors` | Utf8View |
| `access_flags` | UInt32? |
| `opcode_count` | UInt32? |
| `opcode_hex` | Utf8View? |
| `method_kind` | Utf8? |

### `method_opcodes`

Per-method opcode summary.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `opcode_count` | UInt32? |
| `opcode_hex` | Utf8View? |

### `class_defs`

Raw class_def_item records (indices and offsets only).

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_def_idx` | UInt32 |
| `class_idx` | UInt32 |
| `access_flags` | UInt32 |
| `superclass_idx` | UInt32? |
| `interfaces_off` | UInt32 |
| `source_file_idx` | UInt32? |
| `annotations_off` | UInt32 |
| `class_data_off` | UInt32 |
| `static_values_off` | UInt32 |

### `classes`

Class definitions with resolved descriptors.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_def_idx` | UInt32 |
| `class_idx` | UInt32 |
| `descriptor` | Utf8 |
| `access_flags` | UInt32 |
| `superclass_descriptor` | Utf8? |
| `interface_count` | UInt32 |
| `source_file` | Utf8? |

### `class_data_items`

Summary of each class_data_item.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `class_data_off` | UInt32 |
| `class_data_end_off` | UInt32 |
| `static_fields_size` | UInt32 |
| `instance_fields_size` | UInt32 |
| `direct_methods_size` | UInt32 |
| `virtual_methods_size` | UInt32 |

### `class_data_fields`

Field definitions from class_data, with kind and flags.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `field_idx` | UInt32 |
| `access_flags` | UInt32 |
| `kind` | Utf8 |

### `class_data_methods`

Method definitions from class_data, with kind and flags.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |
| `access_flags` | UInt32? |
| `kind` | Utf8? |

### `class_methods`

Methods a class itself declares.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `class_descriptor` | Utf8 |
| `method_idx` | UInt32 |
| `method_name` | Utf8 |
| `method_descriptor` | Utf8 |
| `kind` | Utf8 |
| `access_flags` | UInt32 |

### `class_fields`

Fields a class itself declares.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `class_descriptor` | Utf8 |
| `field_idx` | UInt32 |
| `field_name` | Utf8 |
| `field_descriptor` | Utf8 |
| `kind` | Utf8 |
| `access_flags` | UInt32 |

### `class_interfaces`

Interfaces directly declared by a class.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `ordinal` | UInt32 |
| `interface_type_idx` | UInt32 |
| `interface_descriptor` | Utf8 |

### `type_lists`

Raw type_list structures.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `type_list_off` | UInt32 |
| `type_list_end_off` | UInt32 |
| `size` | UInt32 |

### `type_list_entries`

Per-entry expansion of each type_list.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `type_list_off` | UInt32 |
| `ordinal` | UInt32 |
| `type_idx` | UInt32 |

### `id_items`

Uniform raw offsets of the id sections.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `item_type_code` | UInt32 |
| `idx` | UInt32 |
| `item_off` | UInt32 |
| `item_end_off` | UInt32 |
| `data_off` | UInt32? |

### `map_items`

The DEX map_list.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `ordinal` | UInt32 |
| `type_code` | UInt32 |
| `size` | UInt32 |
| `offset` | UInt32 |

### `method_refs`

Sparse per-instruction reference table over bytecode.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |
| `index_kind` | Utf8 |
| `index` | UInt32 |
| `using_type` | Utf8? |
| `addr` | UInt32 |

### `method_numbers`

Immediate numeric constants loaded by bytecode.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |
| `opcode` | UInt32 |
| `number_type` | Utf8 |
| `long_value` | Int64 |
| `double_value` | Float64? |

### `method_strings`

String constants referenced by each method (one row per instruction).

> 视图：底层是 `_string_refs`，加上 `methods` 等的连接。`value` 由 `strings` 按 `(file_id, string_idx)` 连接得到。

| 列 | 类型 |
| --- | --- |
| `method` | Utf8View |
| `method_class` | Utf8View |
| `method_name` | Utf8View |
| `value` | Utf8View |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |
| `string_idx` | UInt32 |

### `method_calls`

Call instructions: caller columns plus callee columns.

> 视图：底层是 `_call_sites`，加上 `methods` 等的连接。底表保留 `callee` descriptor 以便按被调用者走索引；`method` 三列由 `methods` 连接得到。

| 列 | 类型 |
| --- | --- |
| `method` | Utf8View |
| `method_class` | Utf8View |
| `method_name` | Utf8View |
| `callee` | Utf8View |
| `callee_class` | Utf8View |
| `callee_name` | Utf8View |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |
| `callee_method_idx` | UInt32 |

### `method_field_accesses`

Field read/write instructions.

> 视图：底层是 `_field_accesses`，加上 `methods` 等的连接。`field` 四列由 `fields` 连接得到。

| 列 | 类型 |
| --- | --- |
| `method` | Utf8View |
| `method_class` | Utf8View |
| `method_name` | Utf8View |
| `field` | Utf8View |
| `field_class` | Utf8View |
| `field_name` | Utf8View |
| `field_type` | Utf8View |
| `using_type` | Utf8? |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |
| `field_idx` | UInt32 |

### `method_used_types`

Type-referencing instructions (check-cast, new-instance, ...).

> 视图：底层是 `_used_types`，加上 `methods` 等的连接。`type_descriptor`/`type_name` 由 `types` 连接得到。

| 列 | 类型 |
| --- | --- |
| `method` | Utf8View |
| `method_class` | Utf8View |
| `method_name` | Utf8View |
| `type_descriptor` | Utf8View |
| `type_name` | Utf8View |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |
| `type_idx` | UInt32 |

### `method_using_numbers`

Immediate numeric constants with the using method resolved.

| 列 | 类型 |
| --- | --- |
| `method` | Utf8View |
| `method_class` | Utf8View |
| `method_name` | Utf8View |
| `number_type` | Utf8 |
| `long_value` | Int64 |
| `double_value` | Float64? |
| `opcode` | UInt32 |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `method_idx` | UInt32 |

### `code_items`

code_item header and instruction layout.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `code_off` | UInt32 |
| `registers_size` | UInt32 |
| `ins_size` | UInt32 |
| `outs_size` | UInt32 |
| `tries_size` | UInt32 |
| `debug_info_off` | UInt32 |
| `insns_size` | UInt32 |
| `insns_off` | UInt32 |
| `tries_off` | UInt32? |
| `handlers_off` | UInt32? |

### `try_items`

Protected ranges inside a code item.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `try_idx` | UInt32 |
| `start_addr` | UInt32 |
| `insn_count` | UInt32 |
| `handler_off` | UInt32 |

### `catch_handlers`

Typed catch and catch-all entries.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `handler_idx` | UInt32 |
| `handler_off` | UInt32 |
| `ordinal` | UInt32 |
| `type_idx` | UInt32? |
| `addr` | UInt32 |
| `is_catch_all` | Boolean |

### `debug_info`

Method debug_info_item summary.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `debug_info_off` | UInt32 |
| `line_start` | UInt32 |
| `parameters_size` | UInt32 |

### `debug_parameters`

Parameter names from the debug header.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `debug_info_off` | UInt32 |
| `ordinal` | UInt32 |
| `name_idx` | UInt32? |

### `debug_positions`

Source positions from the debug state machine.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `debug_info_off` | UInt32 |
| `ordinal` | UInt32 |
| `address` | UInt32 |
| `line` | Int32 |
| `source_file_idx` | UInt32? |
| `prologue_end` | Boolean |
| `epilogue_begin` | Boolean |

### `debug_locals`

Local-variable events from the debug state machine.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_idx` | UInt32 |
| `debug_info_off` | UInt32 |
| `ordinal` | UInt32 |
| `address` | UInt32 |
| `event` | Utf8 |
| `register_num` | UInt32 |
| `name_idx` | UInt32? |
| `type_idx` | UInt32? |
| `signature_idx` | UInt32? |

### `annotations`

One row per annotation_item.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `annotation_off` | UInt32 |
| `visibility` | UInt32 |
| `type_idx` | UInt32 |
| `element_count` | UInt32 |

### `annotation_elements`

Encoded-value tree of every annotation item.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `annotation_off` | UInt32 |
| `depth` | UInt32 |
| `ordinal` | UInt32 |
| `name_idx` | UInt32? |
| `value_off` | UInt32 |
| `value_end_off` | UInt32 |
| `value_kind` | Utf8 |
| `value_index` | UInt32? |
| `value_i64` | Int64? |
| `value_f64` | Float64? |

### `annotation_directories`

Summary of class annotation directories.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `annotations_off` | UInt32 |
| `class_annotations_off` | UInt32? |
| `fields_size` | UInt32 |
| `methods_size` | UInt32 |
| `parameters_size` | UInt32 |

### `annotation_directory_entries`

Field/method/parameter entries of annotation directories.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `annotations_off` | UInt32 |
| `entry_kind` | Utf8 |
| `entry_idx` | UInt32 |
| `annotation_set_off` | UInt32? |
| `annotation_set_ref_list_off` | UInt32? |

### `annotation_sets`

Raw annotation_set_item summaries.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `annotation_set_off` | UInt32 |
| `size` | UInt32 |

### `annotation_set_entries`

Per-annotation expansion of annotation sets.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `annotation_set_off` | UInt32 |
| `ordinal` | UInt32 |
| `annotation_off` | UInt32 |

### `annotation_set_ref_lists`

Parameter annotation set ref lists.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `annotation_set_ref_list_off` | UInt32 |
| `size` | UInt32 |

### `annotation_set_refs`

Per-parameter-position entries of a ref list.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `annotation_set_ref_list_off` | UInt32 |
| `ordinal` | UInt32 |
| `annotation_set_off` | UInt32? |

### `annotation_elements_resolved`

Annotation elements joined to their owners.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `owner_kind` | Utf8 |
| `owner_idx` | UInt32 |
| `owner` | Utf8? |
| `parameter_idx` | UInt32? |
| `annotation` | Utf8 |
| `visibility` | UInt32 |
| `element_count` | UInt32 |
| `annotation_off` | UInt32 |
| `element_ordinal` | UInt32? |
| `element_name` | Utf8? |
| `value_kind` | Utf8? |
| `value_index` | UInt32? |
| `value_i64` | Int64? |
| `value_f64` | Float64? |
| `retention_policy` | Utf8? |

### `annotation_values`

Owner-resolved annotation value tree at every depth.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `owner_kind` | Utf8 |
| `owner_idx` | UInt32 |
| `owner_descriptor` | Utf8? |
| `parameter_idx` | UInt32? |
| `annotation_off` | UInt32 |
| `annotation_type_idx` | UInt32 |
| `annotation_descriptor` | Utf8 |
| `depth` | UInt32 |
| `ordinal` | UInt32 |
| `name_idx` | UInt32? |
| `name` | Utf8? |
| `value_kind` | Utf8 |
| `value_i64` | Int64? |
| `value_f64` | Float64? |

### `encoded_arrays`

All discovered encoded_array_items.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `encoded_array_idx` | UInt32 |
| `encoded_array_off` | UInt32 |
| `encoded_array_end_off` | UInt32 |
| `value_count` | UInt32 |

### `encoded_array_values`

Every value node inside an encoded array.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `encoded_array_off` | UInt32 |
| `depth` | UInt32 |
| `ordinal` | UInt32 |
| `name_idx` | UInt32? |
| `value_off` | UInt32 |
| `value_end_off` | UInt32 |
| `value_kind` | Utf8 |
| `value_index` | UInt32? |
| `value_i64` | Int64? |
| `value_f64` | Float64? |

### `encoded_array_owners`

Links encoded arrays to their owners.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `encoded_array_off` | UInt32 |
| `owner_kind` | Utf8 |
| `owner_idx` | UInt32 |

### `class_static_values`

Static field initial values in declaration order.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `class_idx` | UInt32 |
| `static_values_off` | UInt32 |
| `ordinal` | UInt32 |
| `field_idx` | UInt32? |
| `value_kind` | Utf8 |
| `value_index` | UInt32? |
| `value_i64` | Int64? |
| `value_f64` | Float64? |

### `call_sites`

DEX 038+ call_site_id_item summaries.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `call_site_idx` | UInt32 |
| `call_site_off` | UInt32 |
| `value_count` | UInt32 |
| `method_handle_idx` | UInt32? |
| `method_name_idx` | UInt32? |
| `proto_idx` | UInt32? |

### `call_site_args`

Top-level encoded values of a call site.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `call_site_idx` | UInt32 |
| `call_site_off` | UInt32 |
| `ordinal` | UInt32 |
| `value_kind` | Utf8 |
| `value_index` | UInt32? |
| `value_i64` | Int64? |
| `value_f64` | Float64? |

### `method_handles`

DEX 038+ method_handle_item rows.

| 列 | 类型 |
| --- | --- |
| `file_id` | UInt32 |
| `method_handle_idx` | UInt32 |
| `method_handle_type` | UInt32 |
| `member_idx` | UInt32 |

### `hiddenapi_flags`

Android 10+ 把 `hiddenapi_class_data_item` 写进 DEX，用位掩码标注每个字段/方法能否从应用代码调用。这是判断"能不能用这个隐藏 API"的唯一权威来源，比猜测 `@hide` 可靠。

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `file_id` | UInt32 | 所属 DEX |
| `class_idx` | UInt32 | class_defs 下标 |
| `class_descriptor` | Utf8 | 便于直接阅读的类描述符 |
| `member_kind` | Utf8 | `field` 或 `method` |
| `member_idx` | UInt32 | field_idx 或 method_idx |
| `flags` | UInt32 | 原始位掩码 |
| `restriction` | Utf8 | `sdk` / `unsupported` / `blocked` / `core-platform` / `test` |

只写入 `flags != 0` 的成员，所以没有限制的成员不会出现在表里。`restriction` 取最严格的位，规则与 Android 工具链一致。

```sql
-- 这个类里哪些方法被封锁
SELECT m.descriptor, h.restriction
FROM hiddenapi_flags h
JOIN methods m ON m.file_id = h.file_id AND m.method_idx = h.member_idx
WHERE h.member_kind = 'method' AND h.class_descriptor = 'Landroid/os/SystemProperties;';

-- 各限制级别的数量
SELECT restriction, count(*) FROM hiddenapi_flags GROUP BY restriction ORDER BY 2 DESC;
```

表为空有两种可能：包是 debuggable 构建（没有该节），或者所有成员都是 SDK 公开。`SELECT count(*) FROM map_items WHERE type_code = 61440` 可以区分——有 0xF000 却没有行，说明确实没有受限成员。
