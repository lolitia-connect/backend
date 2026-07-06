package document

import "time"

type Document struct {
	Id        int64
	Title     string
	Content   string
	Tags      string
	Show      *bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
