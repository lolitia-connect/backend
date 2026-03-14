package group

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"gorm.io/gorm"
)

type UpdateNodeGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateNodeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateNodeGroupLogic {
	return &UpdateNodeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateNodeGroupLogic) UpdateNodeGroup(req *types.UpdateNodeGroupRequest) error {
	// 检查节点组是否存在
	var nodeGroup group.NodeGroup
	if err := l.svcCtx.DB.Where("id = ?", req.Id).First(&nodeGroup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("node group not found")
		}
		logger.Errorf("failed to find node group: %v", err)
		return err
	}

	// 验证:系统中只能有一个过期节点组
	if req.IsExpiredGroup != nil && *req.IsExpiredGroup {
		var count int64
		err := l.svcCtx.DB.Model(&group.NodeGroup{}).
			Where("is_expired_group = ? AND id != ?", true, req.Id).
			Count(&count).Error
		if err != nil {
			logger.Errorf("failed to check expired group count: %v", err)
			return err
		}
		if count > 0 {
			return errors.New("system already has an expired node group, cannot create multiple")
		}

		// 验证:被订阅商品设置为默认节点组的不能设置为过期节点组
		var subscribeCount int64
		err = l.svcCtx.DB.Model(&subscribe.Subscribe{}).
			Where("node_group_id = ?", req.Id).
			Count(&subscribeCount).Error
		if err != nil {
			logger.Errorf("failed to check subscribe usage: %v", err)
			return err
		}
		if subscribeCount > 0 {
			return errors.New("this node group is used as default node group in subscription products, cannot set as expired group")
		}
	}

	// 构建更新数据
	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Sort != 0 {
		updates["sort"] = req.Sort
	}
	if req.ForCalculation != nil {
		updates["for_calculation"] = *req.ForCalculation
	}
	if req.IsExpiredGroup != nil {
		updates["is_expired_group"] = *req.IsExpiredGroup
		// 过期节点组不参与分组计算
		if *req.IsExpiredGroup {
			updates["for_calculation"] = false
		}
	}
	if req.ExpiredDaysLimit != nil {
		updates["expired_days_limit"] = *req.ExpiredDaysLimit
	}
	if req.MaxTrafficGBExpired != nil {
		updates["max_traffic_gb_expired"] = *req.MaxTrafficGBExpired
	}
	if req.SpeedLimit != nil {
		updates["speed_limit"] = *req.SpeedLimit
	}

	// 获取新的流量区间值
	newMinTraffic := nodeGroup.MinTrafficGB
	newMaxTraffic := nodeGroup.MaxTrafficGB
	if req.MinTrafficGB != nil {
		newMinTraffic = req.MinTrafficGB
		updates["min_traffic_gb"] = *req.MinTrafficGB
	}
	if req.MaxTrafficGB != nil {
		newMaxTraffic = req.MaxTrafficGB
		updates["max_traffic_gb"] = *req.MaxTrafficGB
	}

	// 校验流量区间
	if err := l.validateTrafficRange(int(req.Id), newMinTraffic, newMaxTraffic); err != nil {
		return err
	}

	// 执行更新
	if err := l.svcCtx.DB.Model(&nodeGroup).Updates(updates).Error; err != nil {
		logger.Errorf("failed to update node group: %v", err)
		return err
	}

	logger.Infof("updated node group: id=%d", req.Id)
	return nil
}

// validateTrafficRange 校验流量区间：不能重叠、不能留空档、最小值不能大于最大值
func (l *UpdateNodeGroupLogic) validateTrafficRange(currentNodeGroupId int, newMin, newMax *int64) error {
	// 处理指针值
	minVal := int64(0)
	maxVal := int64(0)
	if newMin != nil {
		minVal = *newMin
	}
	if newMax != nil {
		maxVal = *newMax
	}

	// 检查最小值是否大于最大值
	if minVal > maxVal {
		return errors.New("minimum traffic cannot exceed maximum traffic")
	}

	// 如果两个值都为0，表示不参与流量分组，不需要校验
	if minVal == 0 && maxVal == 0 {
		return nil
	}

	// 查询所有其他设置了流量区间的节点组
	var otherGroups []group.NodeGroup
	if err := l.svcCtx.DB.
		Where("id != ?", currentNodeGroupId).
		Where("(min_traffic_gb > 0 OR max_traffic_gb > 0)").
		Find(&otherGroups).Error; err != nil {
		logger.Errorf("failed to query other node groups: %v", err)
		return err
	}

	// 检查是否有重叠
	for _, other := range otherGroups {
		otherMin := int64(0)
		otherMax := int64(0)
		if other.MinTrafficGB != nil {
			otherMin = *other.MinTrafficGB
		}
		if other.MaxTrafficGB != nil {
			otherMax = *other.MaxTrafficGB
		}

		// 如果对方也没设置区间，跳过
		if otherMin == 0 && otherMax == 0 {
			continue
		}

		// 检查是否有重叠: 如果两个区间相交，就是重叠
		// 不重叠的条件是: newMax <= otherMin OR newMin >= otherMax
		if !(maxVal <= otherMin || minVal >= otherMax) {
			return errors.New("traffic range overlaps with another node group")
		}
	}

	return nil
}
