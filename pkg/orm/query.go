package orm

import (
	"fmt"
	"strings"
)

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
