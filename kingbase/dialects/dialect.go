package kingbase

import (
	"fmt"
	"strings"

	"github.com/jinzhu/gorm"
)

// KbMode defines compatibility mode for Kingbase.
type KbMode string

const (
	KbModeMySQL  KbMode = "mysql"
	KbModeOracle KbMode = "oracle"
)

const (
	DialectKingbaseMySQL  = "kingbase-mysql"
	DialectKingbaseOracle = "kingbase-oracle"
)

func NewKingbaseDialect(mode KbMode) gorm.Dialect {
	base := KingbaseDialect{mode: mode}
	switch mode {
	case KbModeMySQL:
		return &kingbaseMySQLDialect{KingbaseDialect: base}
	case KbModeOracle:
		return &kingbaseOracleDialect{}
	default:
		panic(fmt.Sprintf("unsupported Kingbase mode: %s", mode))
	}
}

func Register() {
	gorm.RegisterDialect(DialectKingbaseMySQL, &kingbaseMySQLDialect{})
	gorm.RegisterDialect(DialectKingbaseOracle, &kingbaseOracleDialect{})
	registerMySQLCallbacks()
	registerQueryCallbacks()
}

func init() {
	Register()
}

func cleanIdentifier(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 {
		switch {
		case name[0] == '"' && name[len(name)-1] == '"':
			return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
		case name[0] == '`' && name[len(name)-1] == '`':
			return strings.ReplaceAll(name[1:len(name)-1], "``", "`")
		case name[0] == '[' && name[len(name)-1] == ']':
			return strings.ReplaceAll(name[1:len(name)-1], "]]", "]")
		}
	}
	return name
}

func splitSchemaTable(table string) (schema, name string) {
	table = strings.TrimSpace(table)
	var quote byte
	componentStart := true
	for i := 0; i < len(table); i++ {
		c := table[i]
		if quote != 0 {
			if c == quote {
				if i+1 < len(table) && table[i+1] == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		if componentStart && (c == '`' || c == '"' || c == '[') {
			quote = c
			if c == '[' {
				quote = ']'
			}
			componentStart = false
			continue
		}
		if c == '.' {
			return cleanIdentifier(table[:i]), cleanIdentifier(table[i+1:])
		}
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			componentStart = false
		}
	}
	return "", cleanIdentifier(table)
}
