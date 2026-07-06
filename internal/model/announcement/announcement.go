package announcement

import "time"

type Announcement struct {
	Id        int64
	Title     string
	Content   string
	Show      *bool
	Pinned    *bool
	Popup     *bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
