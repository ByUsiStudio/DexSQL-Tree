package store

// Views are the relations people query. Five of them (method_calls and friends)
// are not convenience joins but the public face of an internal table that stores
// indices only: a call instruction used to repeat its caller's descriptor on
// every row, which cost ~100 MB of duplicate text on a mid-sized APK. The views
// below restore every original column, in the original order, by joining the
// caller and callee against methods/fields/types/strings.
//
// All joins are LEFT so a malformed index (a callee outside method_ids, which
// the parser records as a warning) still yields a row. An inner join would
// silently drop it and change count(*) — exactly the kind of quiet wrong answer
// this engine tries not to give.
var Views = []Table{
	{Name: "v_methods", Kind: "view", Doc: "methods pre-joined with the Java class name and common access flags.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "class_descriptor Utf8", "java_class Utf8?",
		"name Utf8", "descriptor Utf8", "return_descriptor Utf8", "param_count UInt32",
		"access_flags UInt32?", "opcode_count UInt32?", "method_kind Utf8?",
		"is_public Boolean?", "is_static Boolean?", "is_constructor Boolean?"}},
	{Name: "v_fields", Kind: "view", Doc: "fields pre-joined with the Java class name.", Cols: []string{
		"file_id UInt32", "field_idx UInt32", "class_descriptor Utf8", "java_class Utf8?",
		"name Utf8", "descriptor Utf8", "type_descriptor Utf8"}},
	{Name: "method_calls", Kind: "view", Doc: "Call instructions: caller columns plus callee columns.", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "callee Utf8View",
		"callee_class Utf8View", "callee_name Utf8View", "file_id UInt32", "class_idx UInt32",
		"method_idx UInt32", "callee_method_idx UInt32"}},
	{Name: "method_field_accesses", Kind: "view", Doc: "Field read/write instructions.", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "field Utf8View",
		"field_class Utf8View", "field_name Utf8View", "field_type Utf8View", "using_type Utf8?",
		"file_id UInt32", "class_idx UInt32", "method_idx UInt32", "field_idx UInt32"}},
	{Name: "method_used_types", Kind: "view", Doc: "Type-referencing instructions (check-cast, new-instance, ...).", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "type_descriptor Utf8View",
		"type_name Utf8View", "file_id UInt32", "class_idx UInt32", "method_idx UInt32", "type_idx UInt32"}},
	{Name: "method_strings", Kind: "view", Doc: "String constants referenced by each method (one row per instruction).", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "value Utf8View",
		"file_id UInt32", "class_idx UInt32", "method_idx UInt32", "string_idx UInt32"}},
	{Name: "method_using_numbers", Kind: "view", Doc: "Immediate numeric constants with the using method resolved.", Cols: []string{
		"method Utf8View", "method_class Utf8View", "method_name Utf8View", "number_type Utf8",
		"long_value Int64", "double_value Float64?", "opcode UInt32", "file_id UInt32",
		"class_idx UInt32", "method_idx UInt32"}},
	{Name: "v_overview", Kind: "view", Doc: "One row per relation with its row count; the fastest way to size an input.", Cols: []string{
		"relation Utf8", "row_count Int64"}},
}

var viewDDL = []string{
	`CREATE VIEW IF NOT EXISTS v_methods AS
SELECT m.file_id, m.method_idx, m.class_descriptor, t.java_name AS java_class,
       m.name, m.descriptor, m.return_descriptor, m.param_count,
       m.access_flags, m.opcode_count, m.method_kind,
       (m.access_flags & 1) <> 0 AS is_public,
       (m.access_flags & 8) <> 0 AS is_static,
       (m.access_flags & 65536) <> 0 AS is_constructor
FROM methods m
LEFT JOIN types t ON t.file_id = m.file_id AND t.descriptor = m.class_descriptor`,
	`CREATE VIEW IF NOT EXISTS v_fields AS
SELECT f.file_id, f.field_idx, f.class_descriptor, t.java_name AS java_class,
       f.name, f.descriptor, f.type_descriptor
FROM fields f
LEFT JOIN types t ON t.file_id = f.file_id AND t.descriptor = f.class_descriptor`,
	// _call_sites carries the callee descriptor, so `WHERE callee = ...` can use
	// an index on the base table. The caller columns are plain join results, not
	// COALESCE(...): wrapping them in a function would hide methods.descriptor
	// from the query planner and turn "who calls X" into a full scan. A caller
	// index always resolves (it comes from method_ids), so the columns cannot be
	// NULL in practice.
	`CREATE VIEW IF NOT EXISTS method_calls AS
SELECT m.descriptor      AS method,
       m.class_descriptor AS method_class,
       m.name            AS method_name,
       r.callee          AS callee,
       COALESCE(c.class_descriptor, '') AS callee_class,
       COALESCE(c.name, '')             AS callee_name,
       r.file_id, r.class_idx, r.method_idx, r.callee_method_idx
FROM _call_sites r
LEFT JOIN methods m ON m.file_id = r.file_id AND m.method_idx = r.method_idx
LEFT JOIN methods c ON c.file_id = r.file_id AND c.method_idx = r.callee_method_idx`,
	`CREATE VIEW IF NOT EXISTS method_field_accesses AS
SELECT m.descriptor       AS method,
       m.class_descriptor AS method_class,
       m.name             AS method_name,
       fd.descriptor      AS field,
       fd.class_descriptor AS field_class,
       fd.name            AS field_name,
       fd.type_descriptor AS field_type,
       a.using_type, a.file_id, a.class_idx, a.method_idx, a.field_idx
FROM _field_accesses a
LEFT JOIN methods m ON m.file_id = a.file_id AND m.method_idx = a.method_idx
LEFT JOIN fields fd ON fd.file_id = a.file_id AND fd.field_idx = a.field_idx`,
	`CREATE VIEW IF NOT EXISTS method_used_types AS
SELECT m.descriptor       AS method,
       m.class_descriptor AS method_class,
       m.name             AS method_name,
       COALESCE(t.descriptor, '') AS type_descriptor,
       COALESCE(t.java_name, '')  AS type_name,
       u.file_id, u.class_idx, u.method_idx, u.type_idx
FROM _used_types u
LEFT JOIN methods m ON m.file_id = u.file_id AND m.method_idx = u.method_idx
LEFT JOIN types t ON t.file_id = u.file_id AND t.type_idx = u.type_idx`,
	`CREATE VIEW IF NOT EXISTS method_strings AS
SELECT m.descriptor       AS method,
       m.class_descriptor AS method_class,
       m.name             AS method_name,
       COALESCE(s.value, '') AS value,
       r.file_id, r.class_idx, r.method_idx, r.string_idx
FROM _string_refs r
LEFT JOIN methods m ON m.file_id = r.file_id AND m.method_idx = r.method_idx
LEFT JOIN strings s ON s.string_pool_id = r.file_id AND s.string_idx = r.string_idx`,
	`CREATE VIEW IF NOT EXISTS method_using_numbers AS
SELECT m.descriptor       AS method,
       m.class_descriptor AS method_class,
       m.name             AS method_name,
       n.number_type, n.long_value, n.double_value, n.opcode,
       n.file_id, n.class_idx, n.method_idx
FROM method_numbers n
LEFT JOIN methods m ON m.file_id = n.file_id AND m.method_idx = n.method_idx`,
	// Counting the base tables rather than the views keeps this cheap: the views
	// are the same number of rows by construction (LEFT joins never drop one).
	`CREATE VIEW IF NOT EXISTS v_overview AS
SELECT 'dex_files' AS relation, COUNT(*) AS row_count FROM dex_files
UNION ALL SELECT 'classes', COUNT(*) FROM classes
UNION ALL SELECT 'methods', COUNT(*) FROM methods
UNION ALL SELECT 'fields', COUNT(*) FROM fields
UNION ALL SELECT 'strings', COUNT(*) FROM strings
UNION ALL SELECT 'method_calls', COUNT(*) FROM _call_sites
UNION ALL SELECT 'method_strings', COUNT(*) FROM _string_refs
UNION ALL SELECT 'annotations', COUNT(*) FROM annotations`,
}

// ViewDDL returns the CREATE VIEW statements.
func ViewDDL() []string { return viewDDL }

// AllRelations returns the relations users are meant to see: physical tables
// without the internal ones, followed by every view.
func AllRelations() []Table {
	out := make([]Table, 0, len(Catalog)+len(Views))
	for _, t := range Catalog {
		if !internal(t.Name) {
			out = append(out, t)
		}
	}
	out = append(out, Views...)
	return out
}

// internal reports whether a physical table is an implementation detail.
func internal(name string) bool {
	return len(name) > 0 && name[0] == '_'
}

// Relation looks up a table or a view by name. Internal tables are addressable
// so the playground can show them, but they are not advertised.
func Relation(name string) (Table, bool) {
	if t, ok := CatalogByName[name]; ok {
		return t, true
	}
	for _, v := range Views {
		if v.Name == name {
			return v, true
		}
	}
	return Table{}, false
}
