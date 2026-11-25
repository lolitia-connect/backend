package orm

import (
	"fmt"
	"strings"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

// CSVMember matches one complete value in a legacy comma-separated column.
func CSVMember(field, value string) func(*sql.Selector) {
	return func(s *sql.Selector) {
		if strings.Contains(value, ",") {
			s.Where(sql.False())
			return
		}
		s.Where(sql.P(func(b *sql.Builder) {
			switch s.Dialect() {
			case dialect.MySQL:
				b.WriteString("FIND_IN_SET(").Arg(value).Comma().Ident(field).WriteString(") > 0")
			case dialect.Postgres:
				b.Arg(value).WriteString(" = ANY(string_to_array(").Ident(field).WriteString(", ','))")
			default:
				b.WriteString("instr(',' || ").Ident(field).WriteString(" || ',', ").Arg("," + value + ",").WriteString(") > 0")
			}
		}))
	}
}

func CommaSeparatedContainsCondition(driver, field string, values []string) (string, []any) {
	values = removeEmpty(values)
	if len(values) == 0 {
		return "", nil
	}
	conds := make([]string, len(values))
	args := make([]any, len(values))
	for i, v := range values {
		if NormalizeDriver(driver) == DriverMySQL {
			conds[i] = "FIND_IN_SET(?, " + field + ")"
			args[i] = v
		} else {
			conds[i] = "(',' || COALESCE(" + field + ", '') || ',') LIKE ?"
			args[i] = "%," + v + ",%"
		}
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

func removeEmpty(values []string) []string {
	list := values[:0]
	for _, value := range values {
		if value != "" {
			list = append(list, value)
		}
	}
	return list
}

func TextColumnExpr(driver, field string) string {
	if NormalizeDriver(driver) == DriverPostgres {
		return fmt.Sprintf("CAST(%s AS TEXT)", field)
	}
	return fmt.Sprintf("CAST(%s AS CHAR)", field)
}
