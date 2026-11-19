package log

import modellog "github.com/perfect-panel/server/internal/model/log"

func trafficDateIncluded(filter *modellog.FilterParams, day string) bool {
	switch mode, start, end := filter.ResolveDateRange(); mode {
	case modellog.DateRangeExact:
		return day == start
	case modellog.DateRangeBetween:
		return day >= start && day <= end
	case modellog.DateRangeFrom:
		return day >= start
	case modellog.DateRangeTo:
		return day <= end
	default:
		return true
	}
}
