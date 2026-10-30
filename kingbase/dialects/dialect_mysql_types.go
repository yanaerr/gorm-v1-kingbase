package kingbase

import (
	"regexp"
	"strings"

	"github.com/jinzhu/gorm"
)

// rewriteMysqlType keeps common GORM/MySQL tags usable on Kingbase MySQL mode.
func (k *KingbaseDialect) rewriteMysqlType(f *gorm.StructField) string {
	k.ensureRaw()
	return rewriteMySQLFieldDefinition(k.raw.DataTypeOf(f), f.DBName)
}

var mysqlAutoIncrement = regexp.MustCompile(`(?i)\bAUTO_INCREMENT\b`)

var mysqlDollarQuote = regexp.MustCompile(`^\$(?:[A-Za-z_][A-Za-z_0-9]*)?\$`)

var mysqlIntegerDefinition = regexp.MustCompile(`(?i)^(bigint|mediumint|smallint|tinyint|integer|int)\b(?:\s*\(\s*\d+\s*\))?(?:\s+unsigned\b)?`)

var mysqlUnsignedInteger = regexp.MustCompile(`(?i)^(tinyint|smallint|mediumint|integer|int|bigint)(?:\s*\(\s*\d+\s*\))?\s+unsigned\b`)

// The instance supports native INT/BIGINT UNSIGNED and the TEXT/BLOB families.
// Small unsigned integers need a wider storage type with their original range
// enforced in the database. Column names come from the actual GORM field.
func rewriteMySQLFieldDefinition(definition, column string) string {
	typ, constraint := mysqlDefinitionParts(definition, column)
	if constraint != "" {
		return typ + " " + constraint
	}
	return typ
}

func mysqlUnsignedConstraintName(column string) string {
	return shortenMySQLKeyName("gorm_kb_uint_"+column, 63)
}

func mysqlDefinitionParts(definition, column string) (string, string) {
	definition = strings.TrimSpace(definition)
	unsigned := mysqlUnsignedInteger.FindStringSubmatchIndex(definition)
	translated := rewriteMySQLDefinition(definition)
	if unsigned == nil {
		return translated, ""
	}
	kind := strings.ToLower(definition[unsigned[2]:unsigned[3]])
	maximum, storage := "", ""
	switch kind {
	case "tinyint":
		maximum, storage = "255", "smallint"
	case "smallint":
		maximum, storage = "65535", "integer"
	case "mediumint":
		maximum, storage = "16777215", "integer"
	case "int", "integer":
		maximum = "4294967295"
	case "bigint":
		maximum = "18446744073709551615"
	}
	auto := mysqlAutoIncrement.MatchString(maskMySQLQuotedText(definition))
	if !auto {
		if storage == "" {
			// Display widths do not affect the numeric range. On this instance,
			// INTEGER(11) UNSIGNED silently becomes signed, so normalize aliases
			// and remove display widths before sending the native unsigned type.
			if kind == "integer" || kind == "int" {
				storage = "int unsigned"
			} else {
				storage = "bigint unsigned"
			}
			return storage + definition[unsigned[1]:], ""
		}
		translated = storage + definition[unsigned[1]:]
	}
	constraint := "CONSTRAINT " + quoteMySQLIdentifier(mysqlUnsignedConstraintName(column)) + " CHECK (" + quoteMySQLIdentifier(column) + " BETWEEN 0 AND " + maximum + ")"
	return translated, constraint
}

func rewriteMySQLDefinition(definition string) string {
	definition = strings.TrimSpace(definition)
	integer := mysqlIntegerDefinition.FindStringIndex(definition)
	if integer != nil {
		// Quoted defaults, comments and identifiers are not attributes. Keep
		// byte positions while hiding them from the AUTO_INCREMENT matcher.
		attribute := mysqlAutoIncrement.FindStringIndex(maskMySQLQuotedText(definition))
		if attribute != nil {
			// Native signed AUTO_INCREMENT preserves NULL/zero handling and
			// advances after explicit values. Do not replace it with a sequence
			// default. Unsigned columns still need the checked fallback below.
			if !mysqlUnsignedInteger.MatchString(definition) {
				return definition
			}
			base := "serial"
			kind := strings.ToLower(mysqlIntegerDefinition.FindStringSubmatch(definition)[1])
			if kind == "bigint" || (mysqlUnsignedInteger.MatchString(definition) && (kind == "int" || kind == "integer")) {
				base = "bigserial"
			}
			rest := strings.TrimSpace(definition[integer[1]:attribute[0]] + definition[attribute[1]:])
			if rest != "" {
				base += " " + rest
			}
			return base
		}
	}
	// Keep native types and all application literals. In particular, preserving
	// TINY/MEDIUM/LONG types retains their domain capacity constraints.
	return definition
}

func maskMySQLQuotedText(definition string) string {
	masked := []byte(definition)
	for i := 0; i < len(definition); {
		quote := definition[i]
		start := i
		if strings.HasPrefix(definition[i:], "/*") {
			end := strings.Index(definition[i+2:], "*/")
			if end < 0 {
				i = len(definition)
			} else {
				i += end + 4
			}
		} else if strings.HasPrefix(definition[i:], "--") || quote == '#' {
			end := strings.IndexByte(definition[i:], '\n')
			if end < 0 {
				i = len(definition)
			} else {
				i += end
			}
		} else if delimiter := mysqlDollarQuote.FindString(definition[i:]); delimiter != "" {
			end := strings.Index(definition[i+len(delimiter):], delimiter)
			if end < 0 {
				i = len(definition)
			} else {
				i += end + 2*len(delimiter)
			}
		} else if quote != '\'' && quote != '"' && quote != '`' {
			i++
			continue
		} else {
			i++
			for i < len(definition) {
				if definition[i] == '\\' && i+1 < len(definition) {
					i += 2
				} else if definition[i] == quote {
					i++
					if i < len(definition) && definition[i] == quote {
						i++
						continue
					}
					break
				} else {
					i++
				}
			}
		}
		for n := start; n < i; n++ {
			masked[n] = ' '
		}
	}
	return string(masked)
}
