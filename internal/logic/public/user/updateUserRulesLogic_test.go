package user

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
)

type rulesUsers struct {
	usermodel.Model
	saved string
}

func (model *rulesUsers) Update(_ context.Context, user *usermodel.User) error {
	model.saved = user.Rules
	return nil
}

type rulesStore struct {
	repository.Store
	users *rulesUsers
}

func (store *rulesStore) User() usermodel.Model { return store.users }

func TestUpdateUserRulesCanReplaceAndClearRules(t *testing.T) {
	for _, rules := range [][]string{{"DOMAIN,new.example,DIRECT"}, {}, nil} {
		store := &rulesStore{users: &rulesUsers{}}
		ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 7, Rules: `["DOMAIN,old.example,DIRECT"]`})
		if err := NewUpdateUserRulesLogic(ctx, &svc.ServiceContext{Store: store}).UpdateUserRules(&types.UpdateUserRulesRequest{Rules: rules}); err != nil {
			t.Fatal(err)
		}
		if rules == nil {
			if store.users.saved != "" {
				t.Fatal("omitted rules unexpectedly replaced the saved rules")
			}
			continue
		}
		var stored []string
		if err := json.Unmarshal([]byte(store.users.saved), &stored); err != nil {
			t.Fatalf("rules were not saved: %v", err)
		}
		if len(rules) == 0 {
			if store.users.saved != "[]" {
				t.Fatalf("clearing rules persisted %q instead of an empty list", store.users.saved)
			}
		} else if !reflect.DeepEqual(stored, rules) {
			t.Fatalf("saved rules = %v, want %v", stored, rules)
		}
	}
}
