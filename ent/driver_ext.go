package ent

import "entgo.io/ent/dialect"

func (c *Client) Driver() dialect.Driver {
	return c.driver
}
