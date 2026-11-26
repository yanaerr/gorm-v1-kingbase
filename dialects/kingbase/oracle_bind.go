package kingbase

import (
	"database/sql"
	"fmt"
	"regexp"

	"github.com/jinzhu/gorm"
)

var oracleNamedBindPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// OracleNamedBind creates a named Oracle bind expression such as :name while
// retaining the value as a database/sql named argument for gokb.
func OracleNamedBind(name string, value interface{}) (*gorm.SqlExpr, error) {
	if !oracleNamedBindPattern.MatchString(name) {
		return nil, fmt.Errorf("kingbase-oracle: invalid bind variable name %q", name)
	}
	return gorm.Expr(":"+name, sql.Named(name, value)), nil
}
