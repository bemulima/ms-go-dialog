package http

import (
	"net/http"

	adminhandlers "github.com/bemulima/ms-go-dialog/internal/transport/http/admin/v1/handlers"
	apihandlers "github.com/bemulima/ms-go-dialog/internal/transport/http/api/v1/handlers"
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	privatehandlers "github.com/bemulima/ms-go-dialog/internal/transport/http/private/handlers"
	adminuc "github.com/bemulima/ms-go-dialog/internal/usecase/admin"
	attachmentuc "github.com/bemulima/ms-go-dialog/internal/usecase/attachment"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/go-chi/chi/v5"
)

type RouterDependencies struct {
	AdminService      *adminuc.Service
	DialogService     *dialoguc.Service
	MessageService    *messageuc.Service
	AttachmentService *attachmentuc.Service
	RealtimeService   *realtimeuc.TicketService
	WebSocketHandler  http.Handler
	UserRateLimiter   middleware.ActorLimiter
	Readiness         http.Handler
	Metrics           Metrics
	InternalToken     string
}

// Metrics keeps the transport independent from the concrete observability implementation.
type Metrics interface {
	HTTPMiddleware(http.Handler) http.Handler
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func NewRouter(deps RouterDependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.SecurityHeaders)
	router.Use(middleware.AssignRequestID)
	router.Use(middleware.RecoverPanics(common.WriteError))
	if deps.Metrics != nil {
		router.Use(deps.Metrics.HTTPMiddleware)
	}
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "ms-go-dialog"})
	})
	if deps.Readiness != nil {
		router.Get("/readyz", deps.Readiness.ServeHTTP)
	}
	if deps.Metrics != nil {
		router.Get("/metrics", deps.Metrics.ServeHTTP)
	}
	if deps.AdminService != nil {
		router.Route("/admin/v1", func(admin chi.Router) {
			admin.Use(middleware.RequireActor(common.WriteError))
			if deps.UserRateLimiter != nil {
				admin.Use(middleware.RateLimitActor(deps.UserRateLimiter, common.WriteError))
			}
			handler := adminhandlers.AdminHandler{Service: deps.AdminService}
			admin.Post("/space/create", handler.CreateSpace)
			admin.Get("/space/list", handler.ListSpaces)
			admin.Put("/space/update/{spaceID}", handler.UpdateSpace)
			admin.Get("/dialog/list", handler.ListDialogs)
			admin.Put("/dialog/close/{dialogID}", handler.CloseDialog)
			admin.Put("/dialog/reopen/{dialogID}", handler.ReopenDialog)
			admin.Put("/message/hide/{messageID}", handler.HideMessage)
			admin.Put("/message/restore/{messageID}", handler.RestoreMessage)
		})
	}
	if deps.InternalToken != "" && deps.DialogService != nil && deps.MessageService != nil {
		router.Route("/internal/v1", func(internal chi.Router) {
			internal.Use(middleware.RequireInternalToken(deps.InternalToken, common.WriteError))
			handler := privatehandlers.TeacherDialogHandler{Dialogs: deps.DialogService, Messages: deps.MessageService}
			internal.Put("/teacher-dialog/ensure", handler.Ensure)
			internal.Get("/teacher-dialog/{dialogID}/request/{sourceMessageID}", handler.RequestContext)
			internal.Post("/teacher-dialog/{dialogID}/assistant-ui-source", handler.AssistantUISource)
			internal.Post("/teacher-dialog/{dialogID}/canonical-student-turn", handler.CanonicalStudentTurn)
			internal.Post("/teacher-dialog/{dialogID}/message", handler.AppendResponse)
			internal.Post("/teacher-dialog/{dialogID}/proactive-message", handler.AppendProactive)
			internal.Post("/teacher-dialog/{dialogID}/student-channel-message", handler.AppendStudentChannelMessage)
		})
	}

	router.Route("/api/v1", func(api chi.Router) {
		api.Use(middleware.RequireActor(common.WriteError))
		if deps.UserRateLimiter != nil {
			api.Use(middleware.RateLimitActor(deps.UserRateLimiter, common.WriteError))
		}
		if deps.DialogService != nil {
			handler := apihandlers.DialogHandler{Service: deps.DialogService}
			api.Put("/dialog/personal/ensure", handler.EnsurePersonal)
			api.Post("/dialog/group/create", handler.CreateGroup)
			api.Get("/dialog/list", handler.List)
			api.Get("/dialog/get/{dialogID}", handler.Get)
			api.Put("/dialog/update/{dialogID}", handler.Update)
			api.Post("/dialog/leave/{dialogID}", handler.Leave)
			api.Post("/dialog-member/add/{dialogID}", handler.AddMember)
			api.Delete("/dialog-member/remove/{dialogID}/{userID}", handler.RemoveMember)
			api.Put("/dialog-member/role/{dialogID}/{userID}", handler.ChangeRole)
			api.Put("/user-block/{userID}", handler.BlockUser)
			api.Delete("/user-block/{userID}", handler.UnblockUser)
		}
		if deps.MessageService != nil {
			handler := apihandlers.MessageHandler{Service: deps.MessageService}
			api.Get("/message/window", handler.Window)
			api.Get("/message/list", handler.List)
			api.Get("/message/get/{messageID}", handler.Get)
			api.Get("/message/changes", handler.Changes)
			api.Post("/message/create", handler.Create)
			api.Put("/message/update/{messageID}", handler.Update)
			api.Delete("/message/delete/{messageID}", handler.Delete)
			api.Put("/dialog/read/{dialogID}", handler.Read)
			api.Put("/dialog/read-all/{dialogID}", handler.ReadAll)
		}
		if deps.AttachmentService != nil {
			handler := apihandlers.AttachmentHandler{Service: deps.AttachmentService}
			api.Post("/message-attachment/upload", handler.Upload)
			api.Get("/message-attachment/signed-url/{attachmentID}", handler.SignedURL)
			api.Delete("/message-attachment/delete/{attachmentID}", handler.Delete)
		}
		if deps.RealtimeService != nil {
			handler := apihandlers.RealtimeHandler{Service: deps.RealtimeService}
			api.Post("/realtime/ticket", handler.MintTicket)
		}
	})
	if deps.WebSocketHandler != nil {
		router.Handle("/api/v1/ws", deps.WebSocketHandler)
	}
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		common.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not_found", "message": "route not found"})
	})
	return router
}
