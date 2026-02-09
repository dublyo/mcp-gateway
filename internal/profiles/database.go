package profiles

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

type DatabaseProfile struct{}

func (p *DatabaseProfile) ID() string { return "database" }

func (p *DatabaseProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "query",
			Description: "Execute a read-only SQL query (SELECT only) and return results as a table",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sql": map[string]interface{}{"type": "string", "description": "SQL query to execute (SELECT statements only)"},
				},
				"required": []string{"sql"},
			},
		},
		{
			Name:        "list_tables",
			Description: "List all tables in the database",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"schema": map[string]interface{}{"type": "string", "description": "Schema name (default 'public')"},
				},
			},
		},
		{
			Name:        "describe_table",
			Description: "Show the structure (columns, types, constraints) of a table",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"table":  map[string]interface{}{"type": "string", "description": "Table name"},
					"schema": map[string]interface{}{"type": "string", "description": "Schema name (default 'public')"},
				},
				"required": []string{"table"},
			},
		},
		{
			Name:        "explain_query",
			Description: "Show the execution plan for a SQL query",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sql": map[string]interface{}{"type": "string", "description": "SQL query to explain"},
				},
				"required": []string{"sql"},
			},
		},
	}
}

func (p *DatabaseProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "query":
		return p.query(args, env)
	case "list_tables":
		return p.listTables(args, env)
	case "describe_table":
		return p.describeTable(args, env)
	case "explain_query":
		return p.explainQuery(args, env)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *DatabaseProfile) getDB(env map[string]string) (*sql.DB, error) {
	dsn := env["DATABASE_URL"]
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %s", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	return db, nil
}

func (p *DatabaseProfile) query(args map[string]interface{}, env map[string]string) (string, error) {
	sqlStr := getStr(args, "sql")
	if sqlStr == "" {
		return "", fmt.Errorf("sql is required")
	}

	// Safety: only allow SELECT and WITH (CTE) statements
	normalized := strings.ToUpper(strings.TrimSpace(sqlStr))
	if !strings.HasPrefix(normalized, "SELECT") && !strings.HasPrefix(normalized, "WITH") {
		readOnly := env["READ_ONLY"]
		if readOnly == "" || readOnly == "true" {
			return "", fmt.Errorf("only SELECT queries are allowed (READ_ONLY mode)")
		}
	}

	// Block dangerous statements even in write mode
	for _, kw := range []string{"DROP ", "TRUNCATE ", "ALTER ", "GRANT ", "REVOKE "} {
		if strings.Contains(normalized, kw) {
			return "", fmt.Errorf("%s statements are blocked for safety", strings.TrimSpace(kw))
		}
	}

	db, err := p.getDB(env)
	if err != nil {
		return "", err
	}
	defer db.Close()

	maxRows := 100
	if mr := env["MAX_ROWS"]; mr != "" {
		if n, err := strconv.Atoi(mr); err == nil && n > 0 {
			maxRows = n
		}
	}
	if maxRows > 1000 {
		maxRows = 1000
	}

	// Add LIMIT if not present
	if !strings.Contains(normalized, "LIMIT") {
		sqlStr = sqlStr + fmt.Sprintf(" LIMIT %d", maxRows)
	}

	rows, err := db.Query(sqlStr)
	if err != nil {
		return "", fmt.Errorf("query failed: %s", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %s", err)
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if len(results) == 0 {
		return fmt.Sprintf("Query returned 0 rows\nColumns: %s", strings.Join(columns, ", ")), nil
	}

	output, _ := json.MarshalIndent(results, "", "  ")
	return fmt.Sprintf("Rows: %d\nColumns: %s\n\n%s", len(results), strings.Join(columns, ", "), string(output)), nil
}

func (p *DatabaseProfile) listTables(args map[string]interface{}, env map[string]string) (string, error) {
	schema := getStr(args, "schema")
	if schema == "" {
		schema = "public"
	}

	db, err := p.getDB(env)
	if err != nil {
		return "", err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = $1
		ORDER BY table_name
	`, schema)
	if err != nil {
		return "", fmt.Errorf("query failed: %s", err)
	}
	defer rows.Close()

	var lines []string
	count := 0
	for rows.Next() {
		var name, tableType string
		rows.Scan(&name, &tableType)
		lines = append(lines, fmt.Sprintf("  %s (%s)", name, tableType))
		count++
	}

	if count == 0 {
		return fmt.Sprintf("No tables found in schema '%s'", schema), nil
	}
	return fmt.Sprintf("Tables in '%s' (%d):\n%s", schema, count, strings.Join(lines, "\n")), nil
}

func (p *DatabaseProfile) describeTable(args map[string]interface{}, env map[string]string) (string, error) {
	table := getStr(args, "table")
	if table == "" {
		return "", fmt.Errorf("table is required")
	}
	schema := getStr(args, "schema")
	if schema == "" {
		schema = "public"
	}

	db, err := p.getDB(env)
	if err != nil {
		return "", err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT
			column_name, data_type, character_maximum_length,
			is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schema, table)
	if err != nil {
		return "", fmt.Errorf("query failed: %s", err)
	}
	defer rows.Close()

	var lines []string
	lines = append(lines, fmt.Sprintf("Table: %s.%s\n", schema, table))
	lines = append(lines, fmt.Sprintf("%-30s %-20s %-10s %-30s", "Column", "Type", "Nullable", "Default"))
	lines = append(lines, strings.Repeat("-", 90))

	count := 0
	for rows.Next() {
		var colName, dataType, nullable string
		var maxLen *int
		var defaultVal *string
		rows.Scan(&colName, &dataType, &maxLen, &nullable, &defaultVal)

		typeStr := dataType
		if maxLen != nil {
			typeStr = fmt.Sprintf("%s(%d)", dataType, *maxLen)
		}
		defStr := ""
		if defaultVal != nil {
			defStr = *defaultVal
		}
		lines = append(lines, fmt.Sprintf("%-30s %-20s %-10s %-30s", colName, typeStr, nullable, defStr))
		count++
	}

	if count == 0 {
		return fmt.Sprintf("Table '%s.%s' not found", schema, table), nil
	}

	// Also show indexes
	idxRows, err := db.Query(`
		SELECT indexname, indexdef
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2
	`, schema, table)
	if err == nil {
		defer idxRows.Close()
		lines = append(lines, "")
		lines = append(lines, "Indexes:")
		for idxRows.Next() {
			var name, def string
			idxRows.Scan(&name, &def)
			lines = append(lines, fmt.Sprintf("  %s: %s", name, def))
		}
	}

	return strings.Join(lines, "\n"), nil
}

func (p *DatabaseProfile) explainQuery(args map[string]interface{}, env map[string]string) (string, error) {
	sqlStr := getStr(args, "sql")
	if sqlStr == "" {
		return "", fmt.Errorf("sql is required")
	}

	// EXPLAIN ANALYZE actually executes the query, so enforce same safety checks
	normalized := strings.ToUpper(strings.TrimSpace(sqlStr))
	if !strings.HasPrefix(normalized, "SELECT") && !strings.HasPrefix(normalized, "WITH") {
		readOnly := env["READ_ONLY"]
		if readOnly == "" || readOnly == "true" {
			return "", fmt.Errorf("only SELECT queries can be explained (READ_ONLY mode)")
		}
	}

	for _, kw := range []string{"DROP ", "TRUNCATE ", "ALTER ", "GRANT ", "REVOKE "} {
		if strings.Contains(normalized, kw) {
			return "", fmt.Errorf("%s statements cannot be explained for safety", strings.TrimSpace(kw))
		}
	}

	db, err := p.getDB(env)
	if err != nil {
		return "", err
	}
	defer db.Close()

	rows, err := db.Query("EXPLAIN ANALYZE " + sqlStr)
	if err != nil {
		return "", fmt.Errorf("explain failed: %s", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		rows.Scan(&line)
		lines = append(lines, line)
	}
	return fmt.Sprintf("Query Plan:\n\n%s", strings.Join(lines, "\n")), nil
}
