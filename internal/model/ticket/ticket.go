package ticket

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/perfect-panel/server/pkg/xerr"
)

const (
	Pending              = 1 // Pending  # Pending follow up
	Waiting              = 2 // Waiting  # Waiting for user response
	Processed            = 3 // Processed
	Closed               = 4 // Closed
	FollowText           = 1
	FollowImage          = 2
	MaxTitleLength       = 255
	MaxDescriptionLength = 10000
	MaxFollowLength      = 65535
)

func ValidateTicket(data *Ticket) error {
	if data == nil || data.Title == "" || utf8.RuneCountInString(data.Title) > MaxTitleLength || utf8.RuneCountInString(data.Description) > MaxDescriptionLength {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	return nil
}

func ValidateFollow(data *Follow) error {
	if data == nil || data.TicketId <= 0 || data.From == "" || utf8.RuneCountInString(data.From) > MaxTitleLength || (data.Type != FollowText && data.Type != FollowImage) || data.Content == "" || utf8.RuneCountInString(data.Content) > MaxFollowLength {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	if data.Type == FollowImage && !isImageReference(data.Content) {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	return nil
}

func isImageReference(content string) bool {
	for _, prefix := range []string{"data:image/webp;base64,", "data:image/png;base64,", "data:image/jpeg;base64,", "data:image/gif;base64,"} {
		if len(content) > len(prefix) && strings.EqualFold(content[:len(prefix)], prefix) {
			return true
		}
	}
	parsed, err := url.Parse(content)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

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
