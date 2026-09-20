package settlement

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 维修费用结算模块, 负责费用台账、按班组月份归集结算与流转留痕。
type Module struct {
	repository *Repository
	service    *Service
	handler    *Handler
}

// New 构造费用结算模块。
func New(db *gorm.DB) *Module {
	repository := NewRepository(db)
	service := NewService(repository, db)
	return &Module{
		repository: repository,
		service:    service,
		handler:    NewHandler(service),
	}
}

// Service 暴露模块业务服务, 供维修模块注入锁定端口。
func (m *Module) Service() *Service { return m.service }

// Name 实现 module.Module 接口。
func (m *Module) Name() string { return "维修费用结算" }

// Models 实现 module.Module 接口。
func (m *Module) Models() []any {
	return []any{&Settlement{}, &SettlementItem{}, &SettlementFlow{}}
}

// RegisterRoutes 实现 module.Module 接口。
func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	group := api.Group("/settlements")
	{
		group.GET("", m.handler.List)
		group.POST("", m.handler.Create)
		group.GET("/meta", m.handler.Meta)
		group.GET("/ledger", m.handler.Ledger)
		group.GET("/preview", m.handler.Preview)
		group.GET("/:id", m.handler.Get)
		group.DELETE("/:id", m.handler.Delete)
		group.POST("/:id/submit", m.handler.Submit)
		group.POST("/:id/reject", m.handler.Reject)
		group.POST("/:id/resubmit", m.handler.Resubmit)
		group.POST("/:id/approve", m.handler.Approve)
		group.GET("/:id/diff", m.handler.Diff)
	}
}
