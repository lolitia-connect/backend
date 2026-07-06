package orm

import (
	"strings"
)

const likeEscapeChar = "="

func LikeEscapeClause() string {
	return " ESCAPE '" + likeEscapeChar + "'"
}

func LikePrefixPattern(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return escapeLike(value) + "%"
}

func LikeContainsPattern(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "%" + escapeLike(value) + "%"
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(likeEscapeChar, likeEscapeChar+likeEscapeChar, `%`, likeEscapeChar+`%`, `_`, likeEscapeChar+`_`)
	return replacer.Replace(value)
}
