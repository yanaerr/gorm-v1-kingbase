package kingbase_test

import (
	"errors"
	"testing"

	"github.com/jinzhu/gorm"
	"kingbase.com/gokb"
)

func TestMySQLAdapterExpectedErrorsRejectUnrelatedFailures(t *testing.T) {
	good := &gokb.Error{Code: "23514", Table: "target", Constraint: "gorm_kb_uint_value"}
	var typedNil *gokb.Error
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"target", good, true},
		{"nil", nil, false},
		{"typed_nil", typedNil, false},
		{"connection", errors.New("connection lost"), false},
		{"syntax", &gokb.Error{Code: "42601", Table: "target", Constraint: "gorm_kb_uint_value"}, false},
		{"wrong_table", &gokb.Error{Code: "23514", Table: "other", Constraint: "gorm_kb_uint_value"}, false},
		{"wrong_constraint", &gokb.Error{Code: "23514", Table: "target", Constraint: "other"}, false},
		{"missing_target", &gokb.Error{Code: "23514"}, false},
		{"gorm_target", gorm.Errors{good}, true},
		{"gorm_mixed", gorm.Errors{good, errors.New("rollback failed")}, false},
		{"gorm_empty", gorm.Errors{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := mysqlAdapterMatchesError(test.err, "23514", "target", "gorm_kb_uint_value"); got != test.want {
				t.Fatalf("match=%t, want %t; error=%v", got, test.want, test.err)
			}
		})
	}
}
