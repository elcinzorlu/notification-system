package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/elcinzorlu/notification-system/internal/notification/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository defines the interface for notification persistence.
type Repository interface {
	Create(ctx context.Context, notification *model.Notification) error
	CreateBatch(ctx context.Context, notifications []*model.Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Notification, error)
	GetByBatchID(ctx context.Context, batchID uuid.UUID) ([]model.Notification, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status model.Status, providerMsgID *string, errorMsg *string) error
	Cancel(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter model.ListFilter, page, pageSize int) ([]model.Notification, int64, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*model.Notification, error)
	IncrementAttempts(ctx context.Context, id uuid.UUID) error

	// Template methods
	CreateTemplate(ctx context.Context, template *model.Template) error
	GetTemplateByID(ctx context.Context, id uuid.UUID) (*model.Template, error)
	ListTemplates(ctx context.Context) ([]model.Template, error)
}

type repository struct {
	db *gorm.DB
}

// New creates a new repository instance.
func New(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, notification *model.Notification) error {
	if notification.ID == uuid.Nil {
		notification.ID = uuid.New()
	}
	return r.db.WithContext(ctx).Create(notification).Error
}

func (r *repository) CreateBatch(ctx context.Context, notifications []*model.Notification) error {
	if len(notifications) == 0 {
		return nil
	}
	for _, n := range notifications {
		if n.ID == uuid.Nil {
			n.ID = uuid.New()
		}
	}
	return r.db.WithContext(ctx).CreateInBatches(notifications, 100).Error
}

func (r *repository) GetByID(ctx context.Context, id uuid.UUID) (*model.Notification, error) {
	var notification model.Notification
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&notification).Error
	if err != nil {
		return nil, err
	}
	return &notification, nil
}

func (r *repository) GetByBatchID(ctx context.Context, batchID uuid.UUID) ([]model.Notification, error) {
	var notifications []model.Notification
	err := r.db.WithContext(ctx).Where("batch_id = ?", batchID).Find(&notifications).Error
	return notifications, err
}

func (r *repository) UpdateStatus(ctx context.Context, id uuid.UUID, status model.Status, providerMsgID *string, errorMsg *string) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}
	if providerMsgID != nil {
		updates["provider_message_id"] = *providerMsgID
	}
	if errorMsg != nil {
		updates["error_message"] = *errorMsg
	}
	result := r.db.WithContext(ctx).Model(&model.Notification{}).Where("id = ?", id).Updates(updates)
	if result.RowsAffected == 0 {
		return fmt.Errorf("notification %s not found", id)
	}
	return result.Error
}

func (r *repository) Cancel(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Model(&model.Notification{}).
		Where("id = ? AND status IN ?", id, []model.Status{model.StatusPending, model.StatusQueued}).
		Updates(map[string]interface{}{
			"status":     model.StatusCancelled,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("notification %s not found or cannot be cancelled (only pending/queued)", id)
	}
	return nil
}

func (r *repository) List(ctx context.Context, filter model.ListFilter, page, pageSize int) ([]model.Notification, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := r.db.WithContext(ctx).Model(&model.Notification{})

	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	if filter.Channel != nil {
		query = query.Where("channel = ?", *filter.Channel)
	}
	if filter.DateFrom != nil {
		query = query.Where("created_at >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query = query.Where("created_at <= ?", *filter.DateTo)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var notifications []model.Notification
	offset := (page - 1) * pageSize
	err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&notifications).Error
	return notifications, total, err
}

func (r *repository) GetByIdempotencyKey(ctx context.Context, key string) (*model.Notification, error) {
	var notification model.Notification
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&notification).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &notification, nil
}

func (r *repository) IncrementAttempts(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.Notification{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"attempts":        gorm.Expr("attempts + 1"),
			"last_attempt_at": &now,
			"updated_at":      now,
		}).Error
}

// --- Template Methods ---

func (r *repository) CreateTemplate(ctx context.Context, template *model.Template) error {
	if template.ID == uuid.Nil {
		template.ID = uuid.New()
	}
	return r.db.WithContext(ctx).Create(template).Error
}

func (r *repository) GetTemplateByID(ctx context.Context, id uuid.UUID) (*model.Template, error) {
	var template model.Template
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *repository) ListTemplates(ctx context.Context) ([]model.Template, error) {
	var templates []model.Template
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&templates).Error
	return templates, err
}
