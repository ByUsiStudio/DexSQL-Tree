package store

// Views are convenience relations that pre-join the tables people otherwise
// have to assemble by hand. They are ordinary SQLite views: the query planner
// evaluates them lazily, so an unused view costs nothing, and a filtered query
// against a view still uses the underlying table indexes.
//
// The goal is to make the common questions cheap to ask:
//
//	SELECT java_class, name FROM v_methods WHERE name LIKE '%password%';
//	SELECT * FROM v_overview;
//
// instead of a three-table join written from the schema docs.
var Views = []Table{
	{Name: "v_methods", Kind: "view", Doc: "methods pre-joined with the Java class name and common access flags.", Cols: []string{
		"file_id UInt32", "method_idx UInt32", "class_descriptor Utf8", "java_class Utf8?",
		"name Utf8", "descriptor Utf8", "return_descriptor Utf8", "param_count UInt32",
		"access_flags UInt32?", "opcode_count UInt32?", "method_kind Utf8?",
		"is_public Boolean?", "is_static Boolean?", "is_constructor Boolean?"}},
	{Name: "v_fields", Kind: "view", Doc: "fields pre-joined with the Java class name.", Cols: []string{
		"file_id UInt32", "field_idx UInt32", "class_descriptor Utf8", "java_class Utf8?",
		"name Utf8", "descriptor Utf8", "type_descriptor Utf8"}},
	{Name: "v_overview", Kind: "view", Doc: "One row per relation with its row count; the fastest way to size an input.", Cols: []string{
		"relation Utf8", "row_count Int64"}},
}

var viewDDL = []string{
	`CREATE VIEW v_methods AS
SELECT m.file_id, m.method_idx, m.class_descriptor, t.java_name AS java_class,
       m.name, m.descriptor, m.return_descriptor, m.param_count,
       m.access_flags, m.opcode_count, m.method_kind,
       (m.access_flags & 1) <> 0 AS is_public,
       (m.access_flags & 8) <> 0 AS is_static,
       (m.access_flags & 65536) <> 0 AS is_constructor
FROM methods m
LEFT JOIN types t ON t.file_id = m.file_id AND t.descriptor = m.class_descriptor`,
	`CREATE VIEW v_fields AS
SELECT f.file_id, f.field_idx, f.class_descriptor, t.java_name AS java_class,
       f.name, f.descriptor, f.type_descriptor
FROM fields f
LEFT JOIN types t ON t.file_id = f.file_id AND t.descriptor = f.class_descriptor`,
	`CREATE VIEW v_overview AS
SELECT 'dex_files' AS relation, COUNT(*) AS row_count FROM dex_files
UNION ALL SELECT 'classes', COUNT(*) FROM classes
UNION ALL SELECT 'methods', COUNT(*) FROM methods
UNION ALL SELECT 'fields', COUNT(*) FROM fields
UNION ALL SELECT 'strings', COUNT(*) FROM strings
UNION ALL SELECT 'method_calls', COUNT(*) FROM method_calls
UNION ALL SELECT 'method_strings', COUNT(*) FROM method_strings
UNION ALL SELECT 'annotations', COUNT(*) FROM annotations`,
}

// ViewDDL returns the CREATE VIEW statements.
func ViewDDL() []string { return viewDDL }

// AllRelations returns tables followed by views, which is what the CLI, the
// HTTP API and the MCP server advertise.
func AllRelations() []Table {
	out := make([]Table, 0, len(Catalog)+len(Views))
	out = append(out, Catalog...)
	out = append(out, Views...)
	return out
}

// Relation looks up a table or a view by name.
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
