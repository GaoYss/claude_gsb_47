package settlement

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"streetlight/internal/apperr"
	"streetlight/internal/httpx"
	"streetlight/internal/response"
)

// Handler 处理维修费用结算相关的 HTTP 请求。
type Handler struct {
	service *Service
}

// NewHandler 构造结算处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// List 查询结算单列表。
func (h *Handler) List(c *gin.Context) {
	var query ListQuery
	if err := httpx.BindQuery(c, &query); err != nil {
		response.Fail(c, err)
		return
	}
	items, total, page, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, response.NewPageData(items, total, page.Page, page.PageSize))
}

// Ledger 查询费用台账(已完工维修记录及其结算状态)。
func (h *Handler) Ledger(c *gin.Context) {
	var query RepairRecordQuery
	if err := httpx.BindQuery(c, &query); err != nil {
		response.Fail(c, err)
		return
	}
	items, total, page, summary, err := h.service.Ledger(c.Request.Context(), query)
	if err != nil {
		response.Fail(c, err)
		return
	}
	pageData := response.NewPageData(items, total, page.Page, page.PageSize)
	response.OK(c, gin.H{
		"items":       pageData.Items,
		"total":       pageData.Total,
		"page":        pageData.Page,
		"page_size":   pageData.PageSize,
		"total_pages": pageData.TotalPages,
		"summary":     summary,
	})
}

// Preview 建账前预览指定班组月份的归集结果, 不落库。
func (h *Handler) Preview(c *gin.Context) {
	team := strings.TrimSpace(c.Query("repair_team"))
	period := strings.TrimSpace(c.Query("period"))
	if team == "" {
		response.Fail(c, apperr.BadRequest("repair_team 不能为空"))
		return
	}
	if _, err := parsePeriod(period); err != nil {
		response.Fail(c, err)
		return
	}
	records, err := h.service.Preview(c.Request.Context(), team, period)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, records)
}

// Create 建账。
func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		response.Fail(c, err)
		return
	}
	entity, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, entity)
}

// Get 查询结算单详情。
func (h *Handler) Get(c *gin.Context) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	entity, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, entity)
}

// Delete 删除草稿/已驳回结算单。
func (h *Handler) Delete(c *gin.Context) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Submit 提交结算单。
func (h *Handler) Submit(c *gin.Context) {
	h.handleFlow(c, h.service.Submit)
}

// Reject 驳回结算单。
func (h *Handler) Reject(c *gin.Context) {
	h.handleAudit(c, h.service.Reject)
}

// Resubmit 驳回后重新提交。
func (h *Handler) Resubmit(c *gin.Context) {
	h.handleFlow(c, h.service.Resubmit)
}

// Approve 审核通过。
func (h *Handler) Approve(c *gin.Context) {
	h.handleAudit(c, h.service.Approve)
}

// Diff 对比两个版本的明细差异。
func (h *Handler) Diff(c *gin.Context) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	from, err := parseVersionQuery(c, "from_version")
	if err != nil {
		response.Fail(c, err)
		return
	}
	to, err := parseVersionQuery(c, "to_version")
	if err != nil {
		response.Fail(c, err)
		return
	}
	result, err := h.service.Diff(c.Request.Context(), id, from, to)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, result)
}

// Meta 返回字典与可建账班组/月份。
func (h *Handler) Meta(c *gin.Context) {
	meta, err := h.service.Metadata(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, meta)
}

// handleFlow 处理提交类动作(提交 / 重新提交)。
func (h *Handler) handleFlow(c *gin.Context, fn func(ctx context.Context, id uint, req SubmitRequest) (*Detail, error)) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	var req SubmitRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		response.Fail(c, err)
		return
	}
	entity, err := fn(c.Request.Context(), id, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, entity)
}

// handleAudit 处理审核类动作(驳回 / 通过)。
func (h *Handler) handleAudit(c *gin.Context, fn func(ctx context.Context, id uint, req AuditRequest) (*Detail, error)) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	var req AuditRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		response.Fail(c, err)
		return
	}
	entity, err := fn(c.Request.Context(), id, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, entity)
}

// parseVersionQuery 解析版本号查询参数, 缺省时返回 0 由服务层校验。
func parseVersionQuery(c *gin.Context, name string) (int, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, apperr.BadRequest("%s 不能为空", name)
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, apperr.BadRequest("%s 不是合法的版本号: %q", name, raw)
	}
	return value, nil
}
