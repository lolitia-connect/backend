package redemption

import "strings"

// unitTimeMapping maps lowercase API values to the format expected by tool.AddTime
var unitTimeMapping = map[string]string{
	"day":       "Day",
	"month":     "Month",
	"quarter":   "Quarter",
	"half_year": "HalfYear",
	"year":      "Year",
}

// normalizeUnitTime converts lowercase unit_time to the proper capitalized format
func normalizeUnitTime(unit string) string {
	if normalized, ok := unitTimeMapping[strings.ToLower(unit)]; ok {
		return normalized
	}
	return unit
}
