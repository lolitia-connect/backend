package redemption

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	entredemptioncode "github.com/perfect-panel/server/ent/redemptioncode"
	entredemptionrecord "github.com/perfect-panel/server/ent/redemptionrecord"
)

var _ RedemptionCodeModel = (*customRedemptionCodeModel)(nil)
var _ RedemptionRecordModel = (*customRedemptionRecordModel)(nil)

type (
	RedemptionCodeModel interface {
		Insert(ctx context.Context, data *RedemptionCode) error
		FindOne(ctx context.Context, id int64) (*RedemptionCode, error)
		FindOneByCode(ctx context.Context, code string) (*RedemptionCode, error)
		Update(ctx context.Context, data *RedemptionCode) error
		Delete(ctx context.Context, id int64) error
		customRedemptionCodeLogicModel
	}

	RedemptionRecordModel interface {
		Insert(ctx context.Context, data *RedemptionRecord) error
		FindOne(ctx context.Context, id int64) (*RedemptionRecord, error)
		Update(ctx context.Context, data *RedemptionRecord) error
		Delete(ctx context.Context, id int64) error
		customRedemptionRecordLogicModel
	}

	customRedemptionCodeLogicModel interface {
		QueryRedemptionCodeListByPage(ctx context.Context, page, size int, subscribePlan int64, unitTime string, code string) (total int64, list []*RedemptionCode, err error)
		BatchDelete(ctx context.Context, ids []int64) error
		IncrementUsedCount(ctx context.Context, id int64) error
	}

	customRedemptionRecordLogicModel interface {
		QueryRedemptionRecordListByPage(ctx context.Context, page, size int, userId int64, codeId int64) (total int64, list []*RedemptionRecord, err error)
		FindByUserId(ctx context.Context, userId int64) ([]*RedemptionRecord, error)
		FindByCodeId(ctx context.Context, codeId int64) ([]*RedemptionRecord, error)
	}

	customRedemptionCodeModel struct {
		*defaultRedemptionCodeModel
	}
	defaultRedemptionCodeModel struct {
		db *ent.Client
	}

	customRedemptionRecordModel struct {
		*defaultRedemptionRecordModel
	}
	defaultRedemptionRecordModel struct {
		db *ent.Client
	}
)

func newRedemptionCodeModel(db *ent.Client) *defaultRedemptionCodeModel {
	return &defaultRedemptionCodeModel{db: db}
}

func newRedemptionRecordModel(db *ent.Client) *defaultRedemptionRecordModel {
	return &defaultRedemptionRecordModel{db: db}
}

func (m *defaultRedemptionCodeModel) Insert(ctx context.Context, data *RedemptionCode) error {
	created, err := m.db.RedemptionCode.Create().SetCode(data.Code).SetTotalCount(data.TotalCount).SetUsedCount(data.UsedCount).SetSubscribePlan(data.SubscribePlan).SetUnitTime(data.UnitTime).SetQuantity(data.Quantity).SetStatus(data.Status).Save(ctx)
	if err != nil {
		return err
	}
	copyCodeFromEnt(data, created)
	return nil
}

func (m *defaultRedemptionCodeModel) FindOne(ctx context.Context, id int64) (*RedemptionCode, error) {
	data, err := m.db.RedemptionCode.Query().Where(entredemptioncode.ID(id), entredemptioncode.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return nil, err
	}
	return codeFromEnt(data), nil
}

func (m *defaultRedemptionCodeModel) FindOneByCode(ctx context.Context, code string) (*RedemptionCode, error) {
	data, err := m.db.RedemptionCode.Query().Where(entredemptioncode.Code(code), entredemptioncode.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return nil, err
	}
	return codeFromEnt(data), nil
}

func (m *defaultRedemptionCodeModel) Update(ctx context.Context, data *RedemptionCode) error {
	updated, err := m.db.RedemptionCode.UpdateOneID(data.Id).SetCode(data.Code).SetTotalCount(data.TotalCount).SetUsedCount(data.UsedCount).SetSubscribePlan(data.SubscribePlan).SetUnitTime(data.UnitTime).SetQuantity(data.Quantity).SetStatus(data.Status).Save(ctx)
	if err != nil {
		return err
	}
	copyCodeFromEnt(data, updated)
	return nil
}

func (m *defaultRedemptionCodeModel) Delete(ctx context.Context, id int64) error {
	err := m.db.RedemptionCode.UpdateOneID(id).SetDeletedAt(time.Now()).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *customRedemptionCodeModel) QueryRedemptionCodeListByPage(ctx context.Context, page, size int, subscribePlan int64, unitTime string, code string) (total int64, list []*RedemptionCode, err error) {
	query := m.db.RedemptionCode.Query().Where(entredemptioncode.DeletedAtIsNil())
	if subscribePlan != 0 {
		query = query.Where(entredemptioncode.SubscribePlan(subscribePlan))
	}
	if unitTime != "" {
		query = query.Where(entredemptioncode.UnitTime(unitTime))
	}
	if code != "" {
		query = query.Where(entredemptioncode.CodeContains(code))
	}
	count, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Limit(size).Offset((page - 1) * size).Order(ent.Desc(entredemptioncode.FieldCreatedAt)).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	return int64(count), codesFromEnt(items), nil
}

func (m *customRedemptionCodeModel) BatchDelete(ctx context.Context, ids []int64) error {
	_, err := m.db.RedemptionCode.Update().Where(entredemptioncode.IDIn(ids...), entredemptioncode.DeletedAtIsNil()).SetDeletedAt(time.Now()).Save(ctx)
	return err
}

func (m *customRedemptionCodeModel) IncrementUsedCount(ctx context.Context, id int64) error {
	_, err := m.db.RedemptionCode.Update().Where(entredemptioncode.ID(id), entredemptioncode.DeletedAtIsNil()).AddUsedCount(1).Save(ctx)
	return err
}

func (m *defaultRedemptionRecordModel) Insert(ctx context.Context, data *RedemptionRecord) error {
	created, err := m.db.RedemptionRecord.Create().SetRedemptionCodeID(data.RedemptionCodeId).SetUserID(data.UserId).SetSubscribeID(data.SubscribeId).SetUnitTime(data.UnitTime).SetQuantity(data.Quantity).SetRedeemedAt(data.RedeemedAt).Save(ctx)
	if err != nil {
		return err
	}
	copyRecordFromEnt(data, created)
	return nil
}

func (m *defaultRedemptionRecordModel) FindOne(ctx context.Context, id int64) (*RedemptionRecord, error) {
	data, err := m.db.RedemptionRecord.Query().Where(entredemptionrecord.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return recordFromEnt(data), nil
}

func (m *defaultRedemptionRecordModel) Update(ctx context.Context, data *RedemptionRecord) error {
	updated, err := m.db.RedemptionRecord.UpdateOneID(data.Id).SetRedemptionCodeID(data.RedemptionCodeId).SetUserID(data.UserId).SetSubscribeID(data.SubscribeId).SetUnitTime(data.UnitTime).SetQuantity(data.Quantity).SetRedeemedAt(data.RedeemedAt).Save(ctx)
	if err != nil {
		return err
	}
	copyRecordFromEnt(data, updated)
	return nil
}

func (m *defaultRedemptionRecordModel) Delete(ctx context.Context, id int64) error {
	err := m.db.RedemptionRecord.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *customRedemptionRecordModel) QueryRedemptionRecordListByPage(ctx context.Context, page, size int, userId int64, codeId int64) (total int64, list []*RedemptionRecord, err error) {
	query := m.db.RedemptionRecord.Query()
	if userId != 0 {
		query = query.Where(entredemptionrecord.UserID(userId))
	}
	if codeId != 0 {
		query = query.Where(entredemptionrecord.RedemptionCodeID(codeId))
	}
	count, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Limit(size).Offset((page - 1) * size).Order(ent.Desc(entredemptionrecord.FieldCreatedAt)).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	return int64(count), recordsFromEnt(items), nil
}

func (m *customRedemptionRecordModel) FindByUserId(ctx context.Context, userId int64) ([]*RedemptionRecord, error) {
	items, err := m.db.RedemptionRecord.Query().Where(entredemptionrecord.UserID(userId)).Order(ent.Desc(entredemptionrecord.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	return recordsFromEnt(items), nil
}

func (m *customRedemptionRecordModel) FindByCodeId(ctx context.Context, codeId int64) ([]*RedemptionRecord, error) {
	items, err := m.db.RedemptionRecord.Query().Where(entredemptionrecord.RedemptionCodeID(codeId)).Order(ent.Desc(entredemptionrecord.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	return recordsFromEnt(items), nil
}

func codeFromEnt(data *ent.RedemptionCode) *RedemptionCode {
	if data == nil {
		return nil
	}
	var resp RedemptionCode
	copyCodeFromEnt(&resp, data)
	return &resp
}

func codesFromEnt(items []*ent.RedemptionCode) []*RedemptionCode {
	list := make([]*RedemptionCode, 0, len(items))
	for _, item := range items {
		list = append(list, codeFromEnt(item))
	}
	return list
}

func copyCodeFromEnt(dst *RedemptionCode, src *ent.RedemptionCode) {
	dst.Id = src.ID
	dst.Code = src.Code
	dst.TotalCount = src.TotalCount
	dst.UsedCount = src.UsedCount
	dst.SubscribePlan = src.SubscribePlan
	dst.UnitTime = src.UnitTime
	dst.Quantity = src.Quantity
	dst.Status = src.Status
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

func recordFromEnt(data *ent.RedemptionRecord) *RedemptionRecord {
	if data == nil {
		return nil
	}
	var resp RedemptionRecord
	copyRecordFromEnt(&resp, data)
	return &resp
}

func recordsFromEnt(items []*ent.RedemptionRecord) []*RedemptionRecord {
	list := make([]*RedemptionRecord, 0, len(items))
	for _, item := range items {
		list = append(list, recordFromEnt(item))
	}
	return list
}

func copyRecordFromEnt(dst *RedemptionRecord, src *ent.RedemptionRecord) {
	dst.Id = src.ID
	dst.RedemptionCodeId = src.RedemptionCodeID
	dst.UserId = src.UserID
	dst.SubscribeId = src.SubscribeID
	dst.UnitTime = src.UnitTime
	dst.Quantity = src.Quantity
	dst.RedeemedAt = src.RedeemedAt
	dst.CreatedAt = src.CreatedAt
}
