package handler

import (
	"context"
	"time"

	"github.com/elcinzorlu/notification-system/internal/notification/model"
	"github.com/elcinzorlu/notification-system/internal/notification/service"
	ws "github.com/elcinzorlu/notification-system/internal/websocket"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/gofiber/swagger"
	_ "github.com/elcinzorlu/notification-system/docs/swagger"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Handler handles HTTP requests for the notification API.
type Handler struct {
	service    *service.Service
	wsHub      *ws.Hub
	logger     *zap.Logger
	db         *gorm.DB
	redis      *redis.Client
	rabbitConn *amqp.Connection
}

// New creates a new Handler.
func New(svc *service.Service, hub *ws.Hub, logger *zap.Logger, db *gorm.DB, redisClient *redis.Client, rabbitConn *amqp.Connection) *Handler {
	return &Handler{
		service:    svc,
		wsHub:      hub,
		logger:     logger,
		db:         db,
		redis:      redisClient,
		rabbitConn: rabbitConn,
	}
}

// RegisterRoutes registers all API routes.
func (h *Handler) RegisterRoutes(app *fiber.App) {
	// Middleware
	app.Use(h.correlationIDMiddleware)
	app.Use(h.requestLoggerMiddleware)

	api := app.Group("/api/v1")

	// Notification endpoints
	api.Post("/notifications", h.CreateNotification)
	api.Post("/notifications/batch", h.CreateBatch)
	api.Get("/notifications", h.ListNotifications)
	api.Get("/notifications/:id", h.GetNotification)
	api.Get("/notifications/batch/:batchId", h.GetBatchNotifications)
	api.Patch("/notifications/:id/cancel", h.CancelNotification)

	// Template endpoints
	api.Post("/templates", h.CreateTemplate)
	api.Get("/templates", h.ListTemplates)
	api.Get("/templates/:id", h.GetTemplate)

	// Observability endpoints
	api.Get("/metrics", h.GetMetrics)
	api.Get("/health", h.HealthCheck)

	// Swagger documentation
	app.Get("/swagger/*", swagger.HandlerDefault)

	// WebSocket
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws/notifications", websocket.New(h.wsHub.HandleConnection))
}

// correlationIDMiddleware injects or propagates a correlation ID.
func (h *Handler) correlationIDMiddleware(c *fiber.Ctx) error {
	correlationID := c.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = uuid.New().String()
	}
	c.Locals("correlation_id", correlationID)
	c.Set("X-Correlation-ID", correlationID)
	return c.Next()
}

// requestLoggerMiddleware logs each request with correlation ID.
func (h *Handler) requestLoggerMiddleware(c *fiber.Ctx) error {
	start := time.Now()
	err := c.Next()
	h.logger.Info("HTTP request",
		zap.String("method", c.Method()),
		zap.String("path", c.Path()),
		zap.Int("status", c.Response().StatusCode()),
		zap.Duration("latency", time.Since(start)),
		zap.String("correlation_id", c.Locals("correlation_id").(string)),
	)
	return err
}

// CreateNotification creates a single notification.
// @Summary Create notification
// @Description Create a new notification request
// @Tags notifications
// @Accept json
// @Produce json
// @Param X-Correlation-ID header string false "Correlation ID"
// @Param request body model.CreateRequest true "Notification request"
// @Success 201 {object} model.Notification
// @Failure 400 {object} model.ErrorResponse
// @Failure 500 {object} model.ErrorResponse
// @Router /notifications [post]
func (h *Handler) CreateNotification(c *fiber.Ctx) error {
	var req model.CreateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "invalid_request",
			Message: "Failed to parse request body: " + err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	correlationID := c.Locals("correlation_id").(string)

	notification, err := h.service.Create(c.Context(), req, correlationID)
	if err != nil {
		h.logger.Error("Failed to create notification",
			zap.String("correlation_id", correlationID),
			zap.Error(err),
		)
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "create_failed",
			Message: err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	return c.Status(fiber.StatusCreated).JSON(notification)
}

// CreateBatch creates a batch of notifications.
// @Summary Create batch notifications
// @Description Create up to 1000 notifications in a single batch
// @Tags notifications
// @Accept json
// @Produce json
// @Param X-Correlation-ID header string false "Correlation ID"
// @Param request body model.BatchRequest true "Batch notification request"
// @Success 201 {object} model.BatchResponse
// @Failure 400 {object} model.ErrorResponse
// @Failure 500 {object} model.ErrorResponse
// @Router /notifications/batch [post]
func (h *Handler) CreateBatch(c *fiber.Ctx) error {
	var req model.BatchRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "invalid_request",
			Message: "Failed to parse request body: " + err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	correlationID := c.Locals("correlation_id").(string)

	result, err := h.service.CreateBatch(c.Context(), req, correlationID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "batch_create_failed",
			Message: err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	return c.Status(fiber.StatusCreated).JSON(result)
}

// GetNotification retrieves a notification by ID.
// @Summary Get notification by ID
// @Description Retrieve a notification's details and status
// @Tags notifications
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} model.Notification
// @Failure 404 {object} model.ErrorResponse
// @Router /notifications/{id} [get]
func (h *Handler) GetNotification(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid notification ID format",
			Code:    fiber.StatusBadRequest,
		})
	}

	notification, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(model.ErrorResponse{
			Error:   "not_found",
			Message: "Notification not found",
			Code:    fiber.StatusNotFound,
		})
	}

	return c.JSON(notification)
}

// GetBatchNotifications retrieves all notifications in a batch.
// @Summary Get batch notifications
// @Description Retrieve all notifications in a batch by batch ID
// @Tags notifications
// @Produce json
// @Param batchId path string true "Batch ID"
// @Success 200 {array} model.Notification
// @Failure 404 {object} model.ErrorResponse
// @Router /notifications/batch/{batchId} [get]
func (h *Handler) GetBatchNotifications(c *fiber.Ctx) error {
	batchID, err := uuid.Parse(c.Params("batchId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid batch ID format",
			Code:    fiber.StatusBadRequest,
		})
	}

	notifications, err := h.service.GetByBatchID(c.Context(), batchID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(model.ErrorResponse{
			Error:   "internal_error",
			Message: "Failed to retrieve batch notifications",
			Code:    fiber.StatusInternalServerError,
		})
	}

	return c.JSON(notifications)
}

// CancelNotification cancels a pending/queued notification.
// @Summary Cancel notification
// @Description Cancel a pending or queued notification
// @Tags notifications
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} model.ErrorResponse
// @Failure 404 {object} model.ErrorResponse
// @Router /notifications/{id}/cancel [patch]
func (h *Handler) CancelNotification(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid notification ID format",
			Code:    fiber.StatusBadRequest,
		})
	}

	if err := h.service.Cancel(c.Context(), id); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "cancel_failed",
			Message: err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	return c.JSON(fiber.Map{"status": "cancelled", "id": id.String()})
}

// ListNotifications lists notifications with filtering and pagination.
// @Summary List notifications
// @Description List notifications with optional filters and pagination
// @Tags notifications
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Param status query string false "Filter by status"
// @Param channel query string false "Filter by channel"
// @Param date_from query string false "Filter by start date (RFC3339)"
// @Param date_to query string false "Filter by end date (RFC3339)"
// @Success 200 {object} model.PaginatedResponse
// @Failure 500 {object} model.ErrorResponse
// @Router /notifications [get]
func (h *Handler) ListNotifications(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	pageSize := c.QueryInt("page_size", 20)

	filter := model.ListFilter{}

	if status := c.Query("status"); status != "" {
		s := model.Status(status)
		filter.Status = &s
	}
	if channel := c.Query("channel"); channel != "" {
		ch := model.Channel(channel)
		filter.Channel = &ch
	}
	if dateFrom := c.Query("date_from"); dateFrom != "" {
		t, err := time.Parse(time.RFC3339, dateFrom)
		if err == nil {
			filter.DateFrom = &t
		}
	}
	if dateTo := c.Query("date_to"); dateTo != "" {
		t, err := time.Parse(time.RFC3339, dateTo)
		if err == nil {
			filter.DateTo = &t
		}
	}

	result, err := h.service.List(c.Context(), filter, page, pageSize)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(model.ErrorResponse{
			Error:   "list_failed",
			Message: "Failed to list notifications",
			Code:    fiber.StatusInternalServerError,
		})
	}

	return c.JSON(result)
}

// GetMetrics returns real-time system metrics.
// @Summary Get system metrics
// @Description Get notification system metrics including queue depth, success/failure rates, and latency
// @Tags observability
// @Produce json
// @Success 200 {object} model.MetricsResponse
// @Router /metrics [get]
func (h *Handler) GetMetrics(c *fiber.Ctx) error {
	metricsData := h.service.GetMetrics()
	return c.JSON(metricsData)
}

// HealthCheck checks the health of all dependencies.
// @Summary Health check
// @Description Check the health of PostgreSQL, Redis, and RabbitMQ connections
// @Tags observability
// @Produce json
// @Success 200 {object} model.HealthResponse
// @Failure 503 {object} model.HealthResponse
// @Router /health [get]
func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	resp := model.HealthResponse{
		Status:   "ok",
		DB:       "ok",
		Redis:    "ok",
		RabbitMQ: "ok",
	}
	statusCode := fiber.StatusOK

	// Check PostgreSQL
	sqlDB, err := h.db.DB()
	if err != nil || sqlDB.PingContext(ctx) != nil {
		resp.DB = "unhealthy"
		resp.Status = "degraded"
		statusCode = fiber.StatusServiceUnavailable
	}

	// Check Redis
	if err := h.redis.Ping(ctx).Err(); err != nil {
		resp.Redis = "unhealthy"
		resp.Status = "degraded"
		statusCode = fiber.StatusServiceUnavailable
	}

	// Check RabbitMQ
	if h.rabbitConn == nil || h.rabbitConn.IsClosed() {
		resp.RabbitMQ = "unhealthy"
		resp.Status = "degraded"
		statusCode = fiber.StatusServiceUnavailable
	}

	return c.Status(statusCode).JSON(resp)
}

// --- Template Handlers ---

// CreateTemplate creates a new notification template.
// @Summary Create template
// @Description Create a new notification template with variable substitution support
// @Tags templates
// @Accept json
// @Produce json
// @Param request body model.Template true "Template"
// @Success 201 {object} model.Template
// @Failure 400 {object} model.ErrorResponse
// @Router /templates [post]
func (h *Handler) CreateTemplate(c *fiber.Ctx) error {
	var tmpl model.Template
	if err := c.BodyParser(&tmpl); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "invalid_request",
			Message: "Failed to parse request body: " + err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	if err := h.service.CreateTemplate(c.Context(), &tmpl); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "create_template_failed",
			Message: err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	return c.Status(fiber.StatusCreated).JSON(tmpl)
}

// ListTemplates returns all templates.
// @Summary List templates
// @Description List all notification templates
// @Tags templates
// @Produce json
// @Success 200 {array} model.Template
// @Router /templates [get]
func (h *Handler) ListTemplates(c *fiber.Ctx) error {
	templates, err := h.service.ListTemplates(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(model.ErrorResponse{
			Error:   "list_templates_failed",
			Message: "Failed to list templates",
			Code:    fiber.StatusInternalServerError,
		})
	}
	return c.JSON(templates)
}

// GetTemplate returns a template by ID.
// @Summary Get template by ID
// @Description Retrieve a template's details
// @Tags templates
// @Produce json
// @Param id path string true "Template ID"
// @Success 200 {object} model.Template
// @Failure 404 {object} model.ErrorResponse
// @Router /templates/{id} [get]
func (h *Handler) GetTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(model.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid template ID format",
			Code:    fiber.StatusBadRequest,
		})
	}

	tmpl, err := h.service.GetTemplate(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(model.ErrorResponse{
			Error:   "not_found",
			Message: "Template not found",
			Code:    fiber.StatusNotFound,
		})
	}

	return c.JSON(tmpl)
}
