# Visiomaster Audit: GORM V1 适配 KingbaseES：过程视图

- Style profile: `clean_white`
- Nodes: 32
- Edges: 25
- Containers: 3 (`visible frames`: 3, `dashed_region`: 0, `loss_region`: 0, `audit_region`: 0)

## Typography Review
- Resolved fonts: `Microsoft YaHei UI` (32)
- [ ] Node `page_title` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `page_title` run[0] requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `page_subtitle` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `stage_connection` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `stage_runtime` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `stage_migration` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `deployment_config` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `gorm_open` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `sql_open_ping` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `kingbase_dialect_setdb` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `mode_validation` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `gorm_calls` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `scope_callbacks` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `dialect_sql` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `driver_execute` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `kingbase_es` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `auto_migrate` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `catalog_query` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `ddl_execute` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_choose_dialect` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_sql_open` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_connection_handle` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_startup_validation` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_create_scope` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_sql_request` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_sql_parameters` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_execute` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_has_table_column` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_object_state` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `label_field_definition` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.
- [ ] Node `page_footer` requests font `Microsoft YaHei UI`, which is not installed and has no matching fallback.

## Module Checklist
### `stage_connection`
- Frame: `rectangle` `dash`
- Children (9): `deployment_config`, `gorm_open`, `sql_open_ping`, `kingbase_dialect_setdb`, `mode_validation`, `label_choose_dialect`, `label_sql_open`, `label_connection_handle`, `label_startup_validation`
- Incoming edges (0): none
- Outgoing edges (0): none
- Internal edges (9): `deployment_to_open`, `open_to_ping`, `ping_to_setdb`, `setdb_to_validation`, `accent_deployment_config`, `accent_gorm_open`, `accent_sql_open_ping`, `accent_kingbase_dialect_setdb`, `accent_mode_validation`
- [ ] Compare this module against the source crop: frame bounds, child count, labels, colors, and arrow directions.
- [ ] Check every outgoing edge: does it originate from a component, a boundary, or a bus in the source?

### `stage_runtime`
- Frame: `rectangle` `dash`
- Children (9): `gorm_calls`, `scope_callbacks`, `dialect_sql`, `driver_execute`, `kingbase_es`, `label_create_scope`, `label_sql_request`, `label_sql_parameters`, `label_execute`
- Incoming edges (0): none
- Outgoing edges (0): none
- Internal edges (9): `gorm_to_scope`, `scope_to_sql`, `sql_to_driver`, `driver_to_kingbase`, `accent_gorm_calls`, `accent_scope_callbacks`, `accent_dialect_sql`, `accent_driver_execute`, `accent_kingbase_es`
- [ ] Compare this module against the source crop: frame bounds, child count, labels, colors, and arrow directions.
- [ ] Check every outgoing edge: does it originate from a component, a boundary, or a bus in the source?

### `stage_migration`
- Frame: `rectangle` `dash`
- Children (7): `auto_migrate`, `catalog_query`, `ddl_execute`, `ddl_input_junction`, `label_has_table_column`, `label_object_state`, `label_field_definition`
- Incoming edges (0): none
- Outgoing edges (0): none
- Internal edges (7): `migration_to_catalog`, `catalog_to_ddl`, `migration_to_ddl`, `ddl_junction_stub`, `accent_auto_migrate`, `accent_catalog_query`, `accent_ddl_execute`
- [ ] Compare this module against the source crop: frame bounds, child count, labels, colors, and arrow directions.
- [ ] Check every outgoing edge: does it originate from a component, a boundary, or a bus in the source?

## Topology Review Items
- No obvious topology review items found. Still compare the rendered PNG against the source by module.

## Edge Inventory
- `deployment_to_open`: `lane_arrow` `horizontal` `stage_connection` -> `stage_connection`
- `open_to_ping`: `lane_arrow` `horizontal` `stage_connection` -> `stage_connection`
- `ping_to_setdb`: `lane_arrow` `horizontal` `stage_connection` -> `stage_connection`
- `setdb_to_validation`: `lane_arrow` `horizontal` `stage_connection` -> `stage_connection`
- `gorm_to_scope`: `lane_arrow` `horizontal` `stage_runtime` -> `stage_runtime`
- `scope_to_sql`: `lane_arrow` `horizontal` `stage_runtime` -> `stage_runtime`
- `sql_to_driver`: `dynamic_connector` `straight` `stage_runtime` -> `stage_runtime`
- `driver_to_kingbase`: `lane_arrow` `horizontal` `stage_runtime` -> `stage_runtime`
- `migration_to_catalog`: `lane_arrow` `horizontal` `stage_migration` -> `stage_migration`
- `catalog_to_ddl`: `dynamic_connector` `straight` `stage_migration` -> `stage_migration`
- `migration_to_ddl`: `dynamic_connector` `straight` `stage_migration` -> `stage_migration`
- `ddl_junction_stub`: `line_segment` `vertical` `stage_migration` -> `stage_migration`
- `accent_deployment_config`: `line_segment` `horizontal` `stage_connection` -> `stage_connection`
- `accent_gorm_open`: `line_segment` `horizontal` `stage_connection` -> `stage_connection`
- `accent_sql_open_ping`: `line_segment` `horizontal` `stage_connection` -> `stage_connection`
- `accent_kingbase_dialect_setdb`: `line_segment` `horizontal` `stage_connection` -> `stage_connection`
- `accent_mode_validation`: `line_segment` `horizontal` `stage_connection` -> `stage_connection`
- `accent_gorm_calls`: `line_segment` `horizontal` `stage_runtime` -> `stage_runtime`
- `accent_scope_callbacks`: `line_segment` `horizontal` `stage_runtime` -> `stage_runtime`
- `accent_dialect_sql`: `line_segment` `horizontal` `stage_runtime` -> `stage_runtime`
- `accent_driver_execute`: `line_segment` `horizontal` `stage_runtime` -> `stage_runtime`
- `accent_kingbase_es`: `line_segment` `horizontal` `stage_runtime` -> `stage_runtime`
- `accent_auto_migrate`: `line_segment` `horizontal` `stage_migration` -> `stage_migration`
- `accent_catalog_query`: `line_segment` `horizontal` `stage_migration` -> `stage_migration`
- `accent_ddl_execute`: `line_segment` `horizontal` `stage_migration` -> `stage_migration`
