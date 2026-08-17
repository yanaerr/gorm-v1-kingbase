package kingbase

func (d *kingbaseOracleDialect) hasTableInformationSchema(tableName string) bool {
	schema, name := splitSchemaTable(tableName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM information_schema.tables WHERE LOWER(table_schema) = LOWER(?) AND LOWER(table_name) = LOWER(?)",
			schema, name,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM information_schema.tables WHERE LOWER(table_schema) = LOWER(current_schema()) AND LOWER(table_name) = LOWER(?)",
			name,
		)
	}
	return err == nil && count > 0
}

func (d *kingbaseOracleDialect) hasColumnInformationSchema(tableName, columnName string) bool {
	schema, name := splitSchemaTable(tableName)
	columnName = cleanIdentifier(columnName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM information_schema.columns WHERE LOWER(table_schema) = LOWER(?) AND LOWER(table_name) = LOWER(?) AND LOWER(column_name) = LOWER(?)",
			schema, name, columnName,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM information_schema.columns WHERE LOWER(table_schema) = LOWER(current_schema()) AND LOWER(table_name) = LOWER(?) AND LOWER(column_name) = LOWER(?)",
			name, columnName,
		)
	}
	return err == nil && count > 0
}

func (d *kingbaseOracleDialect) hasIndexNativeCatalog(tableName, indexName string) bool {
	schema, name := splitSchemaTable(tableName)
	indexName = cleanIdentifier(indexName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM pg_indexes WHERE LOWER(schemaname) = LOWER(?) AND LOWER(tablename) = LOWER(?) AND LOWER(indexname) = LOWER(?)",
			schema, name, indexName,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM pg_indexes WHERE LOWER(schemaname) = LOWER(current_schema()) AND LOWER(tablename) = LOWER(?) AND LOWER(indexname) = LOWER(?)",
			name, indexName,
		)
	}
	return err == nil && count > 0
}

func (d *kingbaseOracleDialect) hasForeignKeyInformationSchema(tableName, foreignKeyName string) bool {
	schema, name := splitSchemaTable(tableName)
	foreignKeyName = cleanIdentifier(foreignKeyName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM information_schema.table_constraints WHERE LOWER(table_schema) = LOWER(?) AND LOWER(table_name) = LOWER(?) AND LOWER(constraint_name) = LOWER(?) AND constraint_type = 'FOREIGN KEY'",
			schema, name, foreignKeyName,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM information_schema.table_constraints WHERE LOWER(table_schema) = LOWER(current_schema()) AND LOWER(table_name) = LOWER(?) AND LOWER(constraint_name) = LOWER(?) AND constraint_type = 'FOREIGN KEY'",
			name, foreignKeyName,
		)
	}
	return err == nil && count > 0
}
