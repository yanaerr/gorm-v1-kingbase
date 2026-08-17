# Visiomaster Complexity Report: GORM V1 适配 KingbaseES：过程视图

## Summary
- Style profile: `clean_white`
- Page: 13.33 x 7.50 in, aspect 1.78
- Visible semantic nodes: 27
- Edges: 25
- Regions: 3
- Region-covered visible nodes: 24/27
- Cross-region edges: 0
- Region plan entries: 5
- Validation warnings: 33
- Validation errors: 0

## Source Region Plan
- `global_layout`: ok, source=[0, 0, 2335, 1314], target=[0, 0, 2335, 1314]
- `stage_connection`: ok, source=[73, 219, 2262, 438], target=stage_connection
- `stage_runtime`: ok, source=[73, 511, 1386, 1080], target=stage_runtime
- `stage_migration`: ok, source=[1445, 511, 2262, 1080], target=stage_migration
- `footer_caption`: ok, source=[88, 1205, 2248, 1266], target=[88, 1205, 2248, 1266]

## Recommended Build Mode
- Whole-scene authoring is acceptable, but still run module audit before final Visio render.

## Region Load
- `stage_connection`: 9 visible nodes, density=0.58/sqin, center=(6.67, 1.88) `ok`, source_ar=10.00
- `stage_runtime`: 9 visible nodes, density=0.37/sqin, center=(4.17, 4.54) `ok`, source_ar=2.31
- `stage_migration`: 6 visible nodes, density=0.40/sqin, center=(10.58, 4.54) `ok`, source_ar=1.44
- Uncovered visible nodes: `page_title`, `page_subtitle`, `page_footer`

## Font Scale
- `caption_block`: 20.0-20.0 pt across 1 nodes
- `rounded_process`: 7.0-12.0 pt across 14 nodes
- `text_block`: 8.8-10.5 pt across 12 nodes

## Dense Region Risks
- No region exceeds the default density threshold.

## Paper Detail Grammar Risks
- No compact paper-detail primitives found; if the source has matrices, small operators, ports, or formulas, scene grammar is likely too coarse.

## Validation Snapshot
- WARN: Node `page_title` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `page_title` run[0] requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `page_subtitle` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `stage_connection` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `stage_runtime` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `stage_migration` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `deployment_config` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `gorm_open` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `sql_open_ping` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `kingbase_dialect_setdb` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `mode_validation` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `gorm_calls` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `scope_callbacks` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `dialect_sql` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `driver_execute` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `kingbase_es` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `auto_migrate` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `catalog_query` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `ddl_execute` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `label_choose_dialect` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `label_sql_open` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `label_connection_handle` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `label_startup_validation` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- WARN: Node `label_create_scope` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- Additional validation items suppressed; run `scene_validate.py` for the full list.

