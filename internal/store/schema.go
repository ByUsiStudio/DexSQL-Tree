package store

import (
	"fmt"
	"strings"
)

// Table describes one relation exposed to SQL. Cols entries are "name Type"
// pairs using the same type names as the DexSQL schema reference.
type Table struct {
	Name string   `json:"name"`
	Kind string   `json:"kind"` // "table" or "view"
	Doc  string   `json:"doc"`
	Cols []string `json:"cols"`
}

// Column splits a catalog entry into its name and SQL type label.
func (t Table) Column(i int) (name, typ string) {
	parts := strings.SplitN(t.Cols[i], " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// Columns returns the column names in order.
func (t Table) Columns() []string {
	out := make([]string, len(t.Cols))
	for i := range t.Cols {
		out[i], _ = t.Column(i)
	}
	return out
}

// Catalog is the complete schema, in a stable order.
var Catalog = []Table{
	{Name: "dex_files", Doc: "One row per logical DEX loaded from the input.", Cols: []string{
		"file_id UInt32", "string_pool_id UInt32", "source_path Utf8", "entry_name Utf8", "byte_len UInt64",
		"dex_version Utf8", "checksum UInt32", "signature_hex Utf8", "file_size UInt32", "header_size UInt32",
		"endian_tag UInt32", "link_size UInt32", "link_off UInt32", "map_off UInt32", "string_count UInt32",
		"string_ids_off UInt32", "type_count UInt32", "type_ids_off UInt32", "proto_count UInt32",
		"proto_ids_off UInt32", "field_count UInt32", "field_ids_off UInt32", "method_count UInt32",
		"method_ids_off UInt32", "class_count UInt32", "class_defs_off UInt32", "data_size UInt32", "data_off UInt32"}},
	{Name: "strings", Kind: "table", Doc: "DEX string pool entries; the key is (string_pool_id, string_idx).", Cols: []string{
		"string_pool_id UInt32", "string_idx UInt32", "value Utf8View"}},
	{Name: "type_ids", Doc: "Raw type_ids section (descriptor string index only).", Cols: []string{
		"file_id UInt32", "type_idx UInt32", "descriptor_idx UInt32"}},
	{Name: "types", Doc: "One row per referenced type, with descriptor and Java-style name.", Cols: []string{
		"file_id UInt32", "type_idx UInt32", "descriptor Utf8", "java_name Utf8"}},
	{Name: "proto_ids", Doc: "Raw proto_id_item rows.", Cols: []string{
		"file_id UInt32", "proto_idx UInt32", "shorty_idx UInt32", "return_type_idx UInt32", "parameters_off UInt32"}},
	{Name: "protos", Doc: "Method prototypes with a joined parameter descriptor list.", Cols: []string{
		"file_id UInt32", "proto_idx UInt32", "shorty_idx UInt32", "shorty Utf8", "return_type_idx UInt32",
		"return_descriptor Utf8", "parameters_off UInt32", "param_count UInt32", "param_descriptors Utf8"}},
	{Name: "proto_params", Doc: "One row per prototype parameter.", Cols: []string{
		"file_id UInt32", "proto_idx UInt32", "ordinal UInt32", "type_idx UInt32", "descriptor Utf8"}},
	{Name: "field_ids", Doc: "Raw field_ids section.", Cols: []string{
		"file_id UInt32", "field_idx UInt32", "class_idx UInt32", "type_idx UInt32", "name_idx UInt32"}},
	{Name: "fields", Doc: "Referenced fields with resolved names and full descriptors.", Cols: []string{
		"file_id UInt32", "field_idx UInt32", "class_idx UInt32", "class_descriptor Utf8", "type_idx UInt32",
		"type_descriptor Utf8", "name Utf8", "descriptor Utf8"}},
	{Name: "method_ids", Doc: "Raw method_ids section.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "class_idx UInt32", "proto_idx UInt32", "name_idx UInt32"}},
	{Name: "methods", Doc: "All referenced methods; access/opcode columns are NULL for reference-only rows.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "class_idx UInt32", "class_descriptor Utf8View", "proto_idx UInt32",
		"name Utf8View", "descriptor Utf8View", "return_descriptor Utf8View", "param_count UInt32",
		"param_descriptors Utf8View", "access_flags UInt32?", "opcode_count UInt32?", "opcode_hex Utf8View?",
		"method_kind Utf8?"}},
	{Name: "method_opcodes", Doc: "Per-method opcode summary.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "opcode_count UInt32?", "opcode_hex Utf8View?"}},
	{Name: "class_defs", Doc: "Raw class_def_item records (indices and offsets only).", Cols: []string{
		"file_id UInt32", "class_def_idx UInt32", "class_idx UInt32", "access_flags UInt32",
		"superclass_idx UInt32?", "interfaces_off UInt32", "source_file_idx UInt32?", "annotations_off UInt32",
		"class_data_off UInt32", "static_values_off UInt32"}},
	{Name: "classes", Doc: "Class definitions with resolved descriptors.", Cols: []string{
		"file_id UInt32", "class_def_idx UInt32", "class_idx UInt32", "descriptor Utf8", "access_flags UInt32",
		"superclass_descriptor Utf8?", "interface_count UInt32", "source_file Utf8?"}},
	{Name: "class_data_items", Doc: "Summary of each class_data_item.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "class_data_off UInt32", "class_data_end_off UInt32",
		"static_fields_size UInt32", "instance_fields_size UInt32", "direct_methods_size UInt32", "virtual_methods_size UInt32"}},
	{Name: "class_data_fields", Doc: "Field definitions from class_data, with kind and flags.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "field_idx UInt32", "access_flags UInt32", "kind Utf8"}},
	{Name: "class_data_methods", Doc: "Method definitions from class_data, with kind and flags.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "method_idx UInt32", "access_flags UInt32?", "kind Utf8?"}},
	{Name: "class_methods", Doc: "Methods a class itself declares.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "class_descriptor Utf8", "method_idx UInt32",
		"method_name Utf8", "method_descriptor Utf8", "kind Utf8", "access_flags UInt32"}},
	{Name: "class_fields", Doc: "Fields a class itself declares.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "class_descriptor Utf8", "field_idx UInt32",
		"field_name Utf8", "field_descriptor Utf8", "kind Utf8", "access_flags UInt32"}},
	{Name: "class_interfaces", Doc: "Interfaces directly declared by a class.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "ordinal UInt32", "interface_type_idx UInt32", "interface_descriptor Utf8"}},
	{Name: "type_lists", Doc: "Raw type_list structures.", Cols: []string{
		"file_id UInt32", "type_list_off UInt32", "type_list_end_off UInt32", "size UInt32"}},
	{Name: "type_list_entries", Doc: "Per-entry expansion of each type_list.", Cols: []string{
		"file_id UInt32", "type_list_off UInt32", "ordinal UInt32", "type_idx UInt32"}},
	{Name: "id_items", Doc: "Uniform raw offsets of the id sections.", Cols: []string{
		"file_id UInt32", "item_type_code UInt32", "idx UInt32", "item_off UInt32", "item_end_off UInt32", "data_off UInt32?"}},
	{Name: "map_items", Doc: "The DEX map_list.", Cols: []string{
		"file_id UInt32", "ordinal UInt32", "type_code UInt32", "size UInt32", "offset UInt32"}},
	{Name: "method_refs", Doc: "Sparse per-instruction reference table over bytecode.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "method_idx UInt32", "index_kind Utf8", "index UInt32", "using_type Utf8?", "addr UInt32"}},
	{Name: "method_numbers", Doc: "Immediate numeric constants loaded by bytecode.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "method_idx UInt32", "opcode UInt32", "number_type Utf8",
		"long_value Int64", "double_value Float64?"}},
	{Name: "method_strings", Doc: "String constants referenced by each method (one row per instruction).", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "value Utf8View",
		"file_id UInt32", "class_idx UInt32", "method_idx UInt32", "string_idx UInt32"}},
	{Name: "method_calls", Doc: "Call instructions: caller columns plus callee columns.", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "callee Utf8View",
		"callee_class Utf8View", "callee_name Utf8View", "file_id UInt32", "class_idx UInt32",
		"method_idx UInt32", "callee_method_idx UInt32"}},
	{Name: "method_field_accesses", Doc: "Field read/write instructions.", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "field Utf8View",
		"field_class Utf8View", "field_name Utf8View", "field_type Utf8View", "using_type Utf8?",
		"file_id UInt32", "class_idx UInt32", "method_idx UInt32", "field_idx UInt32"}},
	{Name: "method_used_types", Doc: "Type-referencing instructions (check-cast, new-instance, ...).", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "type_descriptor Utf8View",
		"type_name Utf8View", "file_id UInt32", "class_idx UInt32", "method_idx UInt32", "type_idx UInt32"}},
	{Name: "method_using_numbers", Doc: "Immediate numeric constants with the using method resolved.", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "number_type Utf8",
		"long_value Int64", "double_value Float64?", "opcode UInt32", "file_id UInt32",
		"class_idx UInt32", "method_idx UInt32"}},
	{Name: "code_items", Doc: "code_item header and instruction layout.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "code_off UInt32", "registers_size UInt32", "ins_size UInt32",
		"outs_size UInt32", "tries_size UInt32", "debug_info_off UInt32", "insns_size UInt32",
		"insns_off UInt32", "tries_off UInt32?", "handlers_off UInt32?"}},
	{Name: "try_items", Doc: "Protected ranges inside a code item.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "try_idx UInt32", "start_addr UInt32", "insn_count UInt32", "handler_off UInt32"}},
	{Name: "catch_handlers", Doc: "Typed catch and catch-all entries.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "handler_idx UInt32", "handler_off UInt32", "ordinal UInt32",
		"type_idx UInt32?", "addr UInt32", "is_catch_all Boolean"}},
	{Name: "debug_info", Doc: "Method debug_info_item summary.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "debug_info_off UInt32", "line_start UInt32", "parameters_size UInt32"}},
	{Name: "debug_parameters", Doc: "Parameter names from the debug header.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "debug_info_off UInt32", "ordinal UInt32", "name_idx UInt32?"}},
	{Name: "debug_positions", Doc: "Source positions from the debug state machine.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "debug_info_off UInt32", "ordinal UInt32", "address UInt32",
		"line Int32", "source_file_idx UInt32?", "prologue_end Boolean", "epilogue_begin Boolean"}},
	{Name: "debug_locals", Doc: "Local-variable events from the debug state machine.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "debug_info_off UInt32", "ordinal UInt32", "address UInt32",
		"event Utf8", "register_num UInt32", "name_idx UInt32?", "type_idx UInt32?", "signature_idx UInt32?"}},
	{Name: "annotations", Doc: "One row per annotation_item.", Cols: []string{
		"file_id UInt32", "annotation_off UInt32", "visibility UInt32", "type_idx UInt32", "element_count UInt32"}},
	{Name: "annotation_elements", Doc: "Encoded-value tree of every annotation item.", Cols: []string{
		"file_id UInt32", "annotation_off UInt32", "depth UInt32", "ordinal UInt32", "name_idx UInt32?",
		"value_off UInt32", "value_end_off UInt32", "value_kind Utf8", "value_index UInt32?",
		"value_i64 Int64?", "value_f64 Float64?"}},
	{Name: "annotation_directories", Doc: "Summary of class annotation directories.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "annotations_off UInt32", "class_annotations_off UInt32?",
		"fields_size UInt32", "methods_size UInt32", "parameters_size UInt32"}},
	{Name: "annotation_directory_entries", Doc: "Field/method/parameter entries of annotation directories.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "annotations_off UInt32", "entry_kind Utf8", "entry_idx UInt32",
		"annotation_set_off UInt32?", "annotation_set_ref_list_off UInt32?"}},
	{Name: "annotation_sets", Doc: "Raw annotation_set_item summaries.", Cols: []string{
		"file_id UInt32", "annotation_set_off UInt32", "size UInt32"}},
	{Name: "annotation_set_entries", Doc: "Per-annotation expansion of annotation sets.", Cols: []string{
		"file_id UInt32", "annotation_set_off UInt32", "ordinal UInt32", "annotation_off UInt32"}},
	{Name: "annotation_set_ref_lists", Doc: "Parameter annotation set ref lists.", Cols: []string{
		"file_id UInt32", "annotation_set_ref_list_off UInt32", "size UInt32"}},
	{Name: "annotation_set_refs", Doc: "Per-parameter-position entries of a ref list.", Cols: []string{
		"file_id UInt32", "annotation_set_ref_list_off UInt32", "ordinal UInt32", "annotation_set_off UInt32?"}},
	{Name: "annotation_elements_resolved", Doc: "Annotation elements joined to their owners.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "owner_kind Utf8", "owner_idx UInt32", "owner Utf8?",
		"parameter_idx UInt32?", "annotation Utf8", "visibility UInt32", "element_count UInt32",
		"annotation_off UInt32", "element_ordinal UInt32?", "element_name Utf8?", "value_kind Utf8?",
		"value_index UInt32?", "value_i64 Int64?", "value_f64 Float64?", "retention_policy Utf8?"}},
	{Name: "annotation_values", Doc: "Owner-resolved annotation value tree at every depth.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "owner_kind Utf8", "owner_idx UInt32", "owner_descriptor Utf8?",
		"parameter_idx UInt32?", "annotation_off UInt32", "annotation_type_idx UInt32", "annotation_descriptor Utf8",
		"depth UInt32", "ordinal UInt32", "name_idx UInt32?", "name Utf8?", "value_kind Utf8",
		"value_i64 Int64?", "value_f64 Float64?"}},
	{Name: "encoded_arrays", Doc: "All discovered encoded_array_items.", Cols: []string{
		"file_id UInt32", "encoded_array_idx UInt32", "encoded_array_off UInt32", "encoded_array_end_off UInt32", "value_count UInt32"}},
	{Name: "encoded_array_values", Doc: "Every value node inside an encoded array.", Cols: []string{
		"file_id UInt32", "encoded_array_off UInt32", "depth UInt32", "ordinal UInt32", "name_idx UInt32?",
		"value_off UInt32", "value_end_off UInt32", "value_kind Utf8", "value_index UInt32?",
		"value_i64 Int64?", "value_f64 Float64?"}},
	{Name: "encoded_array_owners", Doc: "Links encoded arrays to their owners.", Cols: []string{
		"file_id UInt32", "encoded_array_off UInt32", "owner_kind Utf8", "owner_idx UInt32"}},
	{Name: "class_static_values", Doc: "Static field initial values in declaration order.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "static_values_off UInt32", "ordinal UInt32", "field_idx UInt32?",
		"value_kind Utf8", "value_index UInt32?", "value_i64 Int64?", "value_f64 Float64?"}},
	{Name: "call_sites", Doc: "DEX 038+ call_site_id_item summaries.", Cols: []string{
		"file_id UInt32", "call_site_idx UInt32", "call_site_off UInt32", "value_count UInt32",
		"method_handle_idx UInt32?", "method_name_idx UInt32?", "proto_idx UInt32?"}},
	{Name: "call_site_args", Doc: "Top-level encoded values of a call site.", Cols: []string{
		"file_id UInt32", "call_site_idx UInt32", "call_site_off UInt32", "ordinal UInt32", "value_kind Utf8",
		"value_index UInt32?", "value_i64 Int64?", "value_f64 Float64?"}},
	{Name: "method_handles", Doc: "DEX 038+ method_handle_item rows.", Cols: []string{
		"file_id UInt32", "method_handle_idx UInt32", "method_handle_type UInt32", "member_idx UInt32"}},
	{Name: "hiddenapi_flags", Doc: "Hidden API restriction per field/method (map_list type 0xF000); empty rows are omitted.", Cols: []string{
		"file_id UInt32", "class_idx UInt32", "class_descriptor Utf8", "member_kind Utf8",
		"member_idx UInt32", "flags UInt32", "restriction Utf8"}},
}

// CatalogByName indexes Catalog.
var CatalogByName = func() map[string]Table {
	m := make(map[string]Table, len(Catalog))
	for _, t := range Catalog {
		m[t.Name] = t
	}
	return m
}()

func sqlType(t string) string {
	switch t {
	case "UInt32", "UInt64", "Int64", "Int32", "Int8", "Boolean":
		return "INTEGER"
	case "Float64":
		return "REAL"
	case "Utf8", "Utf8View", "Dynamic":
		return "TEXT"
	case "":
		return "TEXT"
	}
	return "TEXT"
}

// quote quotes an identifier so reserved words such as "index" can be used as
// column names. Users must quote the same identifier in their queries.
func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// DDL returns the CREATE statements for the whole catalog.
func DDL() []string {
	out := make([]string, 0, len(Catalog)+8)
	for _, t := range Catalog {
		var b strings.Builder
		fmt.Fprintf(&b, "CREATE TABLE %s (", quote(t.Name))
		for i := range t.Cols {
			name, typ := t.Column(i)
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s %s", quote(name), sqlType(typ))
		}
		b.WriteString(");")
		out = append(out, b.String())
	}
	return out
}
