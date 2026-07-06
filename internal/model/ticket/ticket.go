package ticket

import "time"

const (
	Pending   = 1 // Pending  # Pending follow up
	Waiting   = 2 // Waiting  # Waiting for user response
	Processed = 3 // Processed
	Closed    = 4 // Closed
)

type Ticket struct {
	Id          int64
	Title       string
	Description string
	UserId      int64
	Status      uint8
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Follow struct {
	Id        int64
	TicketId  int64
	From      string
	Type      uint8
	Content   string
	CreatedAt time.Time
}
