-- dexsql-tree 示例查询
-- 用法：
--   ./bin/dexsql-tree query --input app.apk --sql "$(sed -n '5,12p' examples/queries.sql)"
-- 或在交互会话中逐条粘贴。

-- 1. 概览：每个 DEX 的类/方法数量
SELECT file_id, entry_name, class_count, method_count, string_count
FROM dex_files ORDER BY file_id;

-- 2. 精确查找方法
SELECT file_id, method_idx, descriptor, access_flags, opcode_count
FROM methods
WHERE descriptor = 'Lcom/example/Foo;->bar(I)V';

-- 3. 按简单名查找可疑入口
SELECT descriptor, access_flags
FROM methods
WHERE name IN ('loadLibrary', 'exec', 'getRuntime')
ORDER BY descriptor;

-- 4. 谁引用了某个字符串（日志、URL、密钥前缀）
SELECT method, value
FROM method_strings
WHERE value LIKE '%api.example.com%';

-- 5. 字符串与调用关系的交集
SELECT DISTINCT c.method
FROM method_strings s
JOIN method_calls c ON c.method = s.method
WHERE s.value LIKE '%token%'
  AND c.callee LIKE '%HttpURLConnection;->connect%';

-- 6. 直接调用者，按调用点数量排序
SELECT method, count(*) AS sites
FROM method_calls
WHERE callee = 'Lcom/example/Foo;->bar(I)V'
GROUP BY method
ORDER BY sites DESC, method;

-- 7. 两跳调用者
SELECT c1.method AS caller, c2.method AS caller_of_caller
FROM method_calls c1
JOIN method_calls c2 ON c2.callee = c1.method
WHERE c1.callee = 'Lcom/example/Foo;->bar(I)V'
ORDER BY c2.method;

-- 8. 字段写入点
SELECT method, field, count(*) AS sites
FROM method_field_accesses
WHERE using_type = 'put' AND field_class = 'Lcom/example/Config;'
GROUP BY method, field;

-- 9. 类自己声明的方法（定义侧）
SELECT method_name, method_descriptor, kind, access_flags
FROM class_methods
WHERE class_descriptor = 'Lcom/example/Foo;'
ORDER BY method_name;

-- 10. 实现某接口的类
SELECT c.descriptor
FROM classes c
JOIN class_interfaces i ON i.file_id = c.file_id AND i.class_idx = c.class_idx
WHERE i.interface_descriptor = 'Ljava/lang/Runnable;';

-- 11. 带运行时注解的类
SELECT DISTINCT owner
FROM annotation_elements_resolved
WHERE owner_kind = 'class'
  AND annotation = 'Lkotlin/Metadata;'
  AND retention_policy = 'runtime';

-- 12. 常量扫描（数字特征）
SELECT method, number_type, long_value
FROM method_using_numbers
WHERE number_type IN ('int', 'long')
  AND long_value BETWEEN 100000 AND 999999
LIMIT 50;

-- 13. 异常处理器覆盖范围
SELECT t.method_idx, t.try_idx, t.start_addr,
       t.start_addr + t.insn_count AS end_addr,
       h.type_idx, h.addr, h.is_catch_all
FROM try_items t
LEFT JOIN catch_handlers h
  ON h.file_id = t.file_id AND h.method_idx = t.method_idx AND h.handler_off = t.handler_off
ORDER BY t.method_idx, t.try_idx, h.ordinal;

-- 14. 方法行号（debug_info 存在时）
SELECT method_idx, address, line
FROM debug_positions
WHERE method_idx = 0
ORDER BY ordinal;

-- 15. 递归 CTE：从某个方法出发的传递调用闭包（最多 4 层）
WITH RECURSIVE closure(method, depth, path) AS (
    SELECT 'Lcom/example/Foo;->bar(I)V', 0, 'Lcom/example/Foo;->bar(I)V'
    UNION ALL
    SELECT c.callee, closure.depth + 1, closure.path || ' -> ' || c.callee
    FROM closure
    JOIN method_calls c ON c.method = closure.method
    WHERE closure.depth < 4
      AND instr(closure.path, c.callee) = 0
)
SELECT DISTINCT method, depth FROM closure ORDER BY depth, method;
