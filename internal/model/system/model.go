package system

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entsystem "github.com/perfect-panel/server/ent/system"
)

type customSystemLogicModel interface {
	GetSmsConfig(ctx context.Context) ([]*System, error)
	GetSiteConfig(ctx context.Context) ([]*System, error)
	GetSubscribeConfig(ctx context.Context) ([]*System, error)
	GetRegisterConfig(ctx context.Context) ([]*System, error)
	GetVerifyConfig(ctx context.Context) ([]*System, error)
	GetNodeConfig(ctx context.Context) ([]*System, error)
	GetInviteConfig(ctx context.Context) ([]*System, error)
	GetTosConfig(ctx context.Context) ([]*System, error)
	GetCurrencyConfig(ctx context.Context) ([]*System, error)
	GetVerifyCodeConfig(ctx context.Context) ([]*System, error)
	GetLogConfig(ctx context.Context) ([]*System, error)
	UpdateValueByCategoryKey(ctx context.Context, category, key, value string) error
	UpdateNodeMultiplierConfig(ctx context.Context, config string) error
	FindNodeMultiplierConfig(ctx context.Context) (*System, error)
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customSystemModel{
		defaultSystemModel: newSystemModel(conn),
	}
}

func (m *customSystemModel) findByCategory(ctx context.Context, category string) ([]*System, error) {
	items, err := m.db.System.Query().Where(entsystem.Category(category)).All(ctx)
	if err != nil {
		return nil, err
	}
	configs := make([]*System, 0, len(items))
	for _, item := range items {
		configs = append(configs, systemFromEnt(item))
	}
	return configs, nil
}

// GetSmsConfig returns the sms config.
func (m *customSystemModel) GetSmsConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "sms")
}

// GetSiteConfig returns the site config.
func (m *customSystemModel) GetSiteConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "site")
}

// GetEmailConfig returns the email config.
func (m *customSystemModel) GetEmailConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "email")
}

// GetSubscribeConfig returns the subscribe config.
func (m *customSystemModel) GetSubscribeConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "subscribe")
}

// GetRegisterConfig returns the register config.
func (m *customSystemModel) GetRegisterConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "register")
}

// GetVerifyConfig returns the verify config.
func (m *customSystemModel) GetVerifyConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "verify")
}

// GetNodeConfig returns the server config.
func (m *customSystemModel) GetNodeConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "server")
}

// GetInviteConfig returns the invite config.
func (m *customSystemModel) GetInviteConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "invite")
}

// GetTelegramConfig returns the telegram config.
func (m *customSystemModel) GetTelegramConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "telegram")
}

// GetTosConfig returns the tos config.
func (m *customSystemModel) GetTosConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "tos")
}

// GetCurrencyConfig returns the currency config.
func (m *customSystemModel) GetCurrencyConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "currency")
}

func (m *customSystemModel) UpdateValueByCategoryKey(ctx context.Context, category, key, value string) error {
	_, err := m.db.System.Update().Where(entsystem.Category(category), entsystem.Key(key)).SetValue(value).Save(ctx)
	return err
}

func (m *customSystemModel) UpdateNodeMultiplierConfig(ctx context.Context, config string) error {
	return m.UpdateValueByCategoryKey(ctx, "server", "NodeMultiplierConfig", config)
}

func (m *customSystemModel) FindNodeMultiplierConfig(ctx context.Context) (*System, error) {
	data, err := m.db.System.Query().Where(entsystem.Category("server"), entsystem.Key("NodeMultiplierConfig")).Only(ctx)
	if err != nil {
		return nil, err
	}
	return systemFromEnt(data), nil
}

// GetVerifyCodeConfig returns the verify code config.

func (m *customSystemModel) GetVerifyCodeConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "verify_code")
}

// GetLogConfig returns the log config.
func (m *customSystemModel) GetLogConfig(ctx context.Context) ([]*System, error) {
	return m.findByCategory(ctx, "log")
}
