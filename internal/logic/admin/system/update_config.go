package system

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"github.com/perfect-panel/server/ent"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

type configFieldValue struct {
	key   string
	value string
	typ   string
}

func convertedConfigFields(data any) []configFieldValue {
	return configFields(data, tool.ConvertValueToString)
}

func stringConfigFields(data any) []configFieldValue {
	return configFields(data, func(value reflect.Value) string {
		return value.String()
	})
}

func configFields(data any, valueFn func(reflect.Value) string) []configFieldValue {
	v := reflect.ValueOf(data)
	t := v.Type()
	fields := make([]configFieldValue, 0, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		typ := v.Field(i).Kind().String()
		switch v.Field(i).Kind() {
		case reflect.String, reflect.Bool, reflect.Int, reflect.Int64:
		case reflect.Ptr:
			typ = v.Field(i).Type().Elem().Kind().String()
		default:
			typ = "interface"
		}
		fields = append(fields, configFieldValue{
			key:   t.Field(i).Name,
			value: valueFn(v.Field(i)),
			typ:   typ,
		})
	}
	return fields
}

func updateConfigFields(ctx context.Context, svcCtx *svc.ServiceContext, category string, fields []configFieldValue, cacheKeys ...string) error {
	err := svcCtx.Store.InTx(ctx, func(store repository.Store) error {
		systemStore := store.System()
		changed := make([]string, 0, len(fields))
		for _, field := range fields {
			previous, err := systemStore.FindByCategoryKey(ctx, category, field.key)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}
			stored := ""
			if previous != nil {
				stored = previous.Value
			}
			field.value, err = keepSettingsSecrets(category, field.key, field.value, stored)
			if err != nil {
				return err
			}
			if previous != nil && previous.Value == field.value {
				continue
			}
			typ := field.typ
			if typ == "" || (category == "server" && (field.key == "DNS" || field.key == "Block" || field.key == "Outbound")) {
				typ = "string"
			}
			if previous != nil && previous.Type != "" {
				typ = previous.Type
			}
			if err := systemStore.UpsertByCategoryKey(ctx, category, field.key, field.value, typ, ""); err != nil {
				return err
			}
			changed = append(changed, field.key)
		}
		if len(changed) == 0 {
			return nil
		}
		sort.Strings(changed)
		entry, err := modellog.NewAdminActionLog(ctx, modellog.AdminAction{Action: "settings.update", Object: category, Detail: "keys=" + strings.Join(changed, ",")})
		if err != nil {
			return err
		}
		return store.Log().Insert(ctx, entry)
	})
	if err != nil {
		return err
	}
	if len(cacheKeys) > 0 && svcCtx.Redis != nil {
		return svcCtx.Redis.Del(ctx, cacheKeys...).Err()
	}
	return nil
}
