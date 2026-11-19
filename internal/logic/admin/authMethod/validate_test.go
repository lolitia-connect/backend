package authMethod

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/sms"
)

type configAuthModel struct {
	auth.Model
	current auth.Auth
	updates int
}

func (model *configAuthModel) FindOneByMethod(context.Context, string) (*auth.Auth, error) {
	copy := model.current
	return &copy, nil
}

func (model *configAuthModel) Update(_ context.Context, current *auth.Auth) error {
	if current.Id != model.current.Id {
		return errors.New("configuration row not found")
	}
	model.current = *current
	model.updates++
	return nil
}

type configStore struct {
	repository.Store
	model *configAuthModel
}

func (store *configStore) Auth() auth.Model { return store.model }

func TestAuthMethodTogglePreservesConfiguration(t *testing.T) {
	enabled := true
	store := &configStore{model: &configAuthModel{current: auth.Auth{Id: 7, Method: "github", Config: `{"client_id":"fixture-id","client_secret":"fixture-secret"}`, Enabled: &enabled}}}
	logic := NewUpdateAuthMethodConfigLogic(context.Background(), &svc.ServiceContext{Store: store})
	for _, value := range []bool{false, true} {
		if _, err := logic.UpdateAuthMethodConfig(&types.UpdateAuthMethodConfigRequest{Method: "github", Enabled: &value}); err != nil {
			t.Fatal(err)
		}
		if *store.model.current.Enabled != value || store.model.current.Config != `{"client_id":"fixture-id","client_secret":"fixture-secret"}` {
			t.Fatal("toggling login method cleared its saved configuration")
		}
	}
}

func TestAuthMethodRejectsMalformedConfigurationBeforeSaving(t *testing.T) {
	for _, config := range []any{"not-an-object", map[string]any{"enable_verify": "not-a-boolean"}} {
		enabled := true
		store := &configStore{model: &configAuthModel{current: auth.Auth{Id: 7, Method: "email", Config: `{"platform":"smtp"}`, Enabled: &enabled}}}
		logic := NewUpdateAuthMethodConfigLogic(context.Background(), &svc.ServiceContext{Store: store})
		if _, err := logic.UpdateAuthMethodConfig(&types.UpdateAuthMethodConfigRequest{Id: 7, Method: "email", Config: config}); err == nil {
			t.Fatal("malformed configuration was accepted")
		}
		if store.model.updates != 0 {
			t.Fatal("invalid configuration overwrote saved settings")
		}
	}
}

func TestValidate(t *testing.T) {
	config := " {\"0\":\"{\",\"1\":\"\\\"\",\"10\":\"y\",\"11\":\"I\",\"12\":\"d\",\"13\":\"\\\"\",\"14\":\":\",\"15\":\"\\\"\",\"16\":\"\\\"\",\"17\":\",\",\"18\":\"\\\"\",\"19\":\"A\",\"2\":\"A\",\"20\":\"c\",\"21\":\"c\",\"22\":\"e\",\"23\":\"s\",\"24\":\"s\",\"25\":\"K\",\"26\":\"e\",\"27\":\"y\",\"28\":\"S\",\"29\":\"e\",\"3\":\"c\",\"30\":\"c\",\"31\":\"r\",\"32\":\"e\",\"33\":\"t\",\"34\":\"\\\"\",\"35\":\":\",\"36\":\"\\\"\",\"37\":\"\\\"\",\"38\":\",\",\"39\":\"\\\"\",\"4\":\"c\",\"40\":\"S\",\"41\":\"i\",\"42\":\"g\",\"43\":\"n\",\"44\":\"N\",\"45\":\"a\",\"46\":\"m\",\"47\":\"e\",\"48\":\"\\\"\",\"49\":\":\",\"5\":\"e\",\"50\":\"\\\"\",\"51\":\"\\\"\",\"52\":\",\",\"53\":\"\\\"\",\"54\":\"E\",\"55\":\"n\",\"56\":\"d\",\"57\":\"p\",\"58\":\"o\",\"59\":\"i\",\"6\":\"s\",\"60\":\"n\",\"61\":\"t\",\"62\":\"\\\"\",\"63\":\":\",\"64\":\"\\\"\",\"65\":\"\\\"\",\"66\":\",\",\"67\":\"\\\"\",\"68\":\"V\",\"69\":\"e\",\"7\":\"s\",\"70\":\"r\",\"71\":\"i\",\"72\":\"f\",\"73\":\"y\",\"74\":\"T\",\"75\":\"e\",\"76\":\"m\",\"77\":\"p\",\"78\":\"l\",\"79\":\"a\",\"8\":\"K\",\"80\":\"t\",\"81\":\"e\",\"82\":\"C\",\"83\":\"o\",\"84\":\"d\",\"85\":\"e\",\"86\":\"\\\"\",\"87\":\":\",\"88\":\"\\\"\",\"89\":\"\\\"\",\"9\":\"e\",\"90\":\"}\",\"access\":\"xxxx\",\"secret\":\"SSxxxxxxxxxxxxxxxxxxxxxxxU\",\"template\":\"Your verification code is: {{.code}}\"}"
	var mapConfig map[string]interface{}
	if err := json.Unmarshal([]byte(config), &mapConfig); err != nil {
		t.Error(err)
	}
	platformConfig, err := validatePlatformConfig(sms.Abosend.String(), mapConfig)
	if err != nil {
		t.Errorf("validateEmailPlatformConfig error: %v", err)
	}
	t.Logf("platformConfig: %+v", platformConfig)
}
