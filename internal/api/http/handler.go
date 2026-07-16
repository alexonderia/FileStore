package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/alexonderia/filestore/internal/api/problem"
	"github.com/alexonderia/filestore/internal/domain"
	"github.com/alexonderia/filestore/internal/service"
	"github.com/alexonderia/filestore/internal/storage"
)

const maxJSONBody = 1 << 20

type IdentityService interface {
	Register(context.Context, string, string, string) (domain.AuthResult, error)
	Login(context.Context, string, string) (domain.AuthResult, error)
	Authenticate(context.Context, string) (domain.Actor, error)
	Logout(context.Context, string) error
}

type WorkspaceService interface {
	Base(context.Context, domain.User) (domain.Workspace, error)
	List(context.Context, domain.User) ([]domain.Workspace, error)
	Get(context.Context, domain.User, string) (domain.Workspace, error)
	Create(context.Context, domain.User, string) (domain.Workspace, error)
	PutMember(context.Context, domain.User, string, string, string, domain.WorkspaceRole) (domain.WorkspaceMember, error)
	RemoveMember(context.Context, domain.User, string, string) error
}

type FileService interface {
	Create(context.Context, domain.User, string, string, string, string, io.Reader) (domain.File, error)
	List(context.Context, domain.User, string, string, int) (domain.FilePage, error)
	Get(context.Context, domain.User, string) (domain.File, error)
	History(context.Context, domain.User, string, string, int) (domain.VersionPage, error)
	Download(context.Context, domain.User, string, int) (domain.FileVersion, storage.Object, error)
	SetEncoding(context.Context, domain.User, string, string) (domain.File, error)
}

type UpdateService interface {
	Create(context.Context, domain.User, string, string, string, io.Reader) (domain.UpdateSession, error)
	Diff(context.Context, domain.User, string, string) (domain.DiffResult, error)
	Resolve(context.Context, domain.User, string, string) (domain.FileVersion, error)
	Reject(context.Context, domain.User, string, string) (domain.UpdateSession, error)
}

type LockService interface {
	Get(context.Context, domain.User, string) (domain.FileLock, error)
	Create(context.Context, domain.User, string) (domain.FileLock, error)
	Release(context.Context, domain.User, string) (domain.FileLock, error)
}

type LinkService interface {
	List(context.Context, domain.User, string, string, int) (domain.LinkPage, error)
	Revoke(context.Context, domain.User, string, string) error
	Download(context.Context, *domain.User, string) (domain.FileVersion, storage.Object, error)
}

type handler struct {
	identity   IdentityService
	workspaces WorkspaceService
	files      FileService
	updates    UpdateService
	locks      LockService
	links      LinkService
	maxUpload  int64
}

type healthResponse struct {
	Status string `json:"status"`
}

func NewHandler() http.Handler {
	return newHandler(nil, nil, nil, nil, nil, nil, 0)
}

func NewProductHandler(identity IdentityService, workspaces WorkspaceService) http.Handler {
	return newHandler(identity, workspaces, nil, nil, nil, nil, 0)
}

func NewMVPHandler(identity IdentityService, workspaces WorkspaceService, files FileService, maxUpload int64) http.Handler {
	return newHandler(identity, workspaces, files, nil, nil, nil, maxUpload)
}

func NewFullHandler(identity IdentityService, workspaces WorkspaceService, files FileService, updates UpdateService, locks LockService, links LinkService, maxUpload int64) http.Handler {
	return newHandler(identity, workspaces, files, updates, locks, links, maxUpload)
}

func newHandler(identity IdentityService, workspaces WorkspaceService, files FileService, updates UpdateService, locks LockService, links LinkService, maxUpload int64) http.Handler {
	h := &handler{identity: identity, workspaces: workspaces, files: files, updates: updates, locks: locks, links: links, maxUpload: maxUpload}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health("live"))
	mux.HandleFunc("GET /health/ready", health("ready"))
	if identity != nil && workspaces != nil {
		mux.HandleFunc("POST /auth/register", h.register)
		mux.HandleFunc("POST /auth/login", h.login)
		mux.HandleFunc("POST /auth/logout", h.logout)
		mux.HandleFunc("GET /auth/me", h.me)
		mux.HandleFunc("GET /workspaces/base", h.baseWorkspace)
		mux.HandleFunc("GET /workspaces", h.listWorkspaces)
		mux.HandleFunc("POST /workspaces", h.createWorkspace)
		mux.HandleFunc("GET /workspaces/{workspaceId}", h.getWorkspace)
		mux.HandleFunc("POST /workspaces/{workspaceId}/members", h.putWorkspaceMemberByEmail)
		mux.HandleFunc("PUT /workspaces/{workspaceId}/members/{userId}", h.putWorkspaceMemberByID)
		mux.HandleFunc("DELETE /workspaces/{workspaceId}/members/{userId}", h.removeWorkspaceMember)
		if files != nil {
			mux.HandleFunc("GET /workspaces/{workspaceId}/files", h.listFiles)
			mux.HandleFunc("POST /workspaces/{workspaceId}/files", h.createFile)
			mux.HandleFunc("GET /files/{fileId}", h.getFile)
			mux.HandleFunc("GET /files/{fileId}/encoding", h.getFileEncoding)
			mux.HandleFunc("PATCH /files/{fileId}/encoding", h.setFileEncoding)
			mux.HandleFunc("GET /files/{fileId}/versions", h.fileHistory)
			mux.HandleFunc("GET /files/{fileId}/content", h.downloadFile)
			if updates != nil {
				mux.HandleFunc("POST /files/{fileId}/updates", h.createUpdate)
				mux.HandleFunc("GET /files/{fileId}/updates/{sessionId}/diff", h.diffUpdate)
				mux.HandleFunc("POST /files/{fileId}/updates/{sessionId}/resolve", h.resolveUpdate)
				mux.HandleFunc("POST /files/{fileId}/updates/{sessionId}/reject", h.rejectUpdate)
			}
			if locks != nil {
				mux.HandleFunc("GET /files/{fileId}/lock", h.getLock)
				mux.HandleFunc("POST /files/{fileId}/lock", h.createLock)
				mux.HandleFunc("DELETE /files/{fileId}/lock", h.releaseLock)
			}
			if links != nil {
				mux.HandleFunc("GET /files/{fileId}/links", h.listLinks)
				mux.HandleFunc("DELETE /files/{fileId}/links/{linkId}", h.revokeLink)
			}
		}
	}
	if links != nil {
		mux.HandleFunc("GET /links/{token}/content", h.downloadLink)
	}
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health/live" || request.URL.Path == "/health/ready" {
			writer.Header().Set("Allow", http.MethodGet)
			writeError(writer, request, service.ErrInvalid, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed for this resource")
			return
		}
		writeError(writer, request, service.ErrNotFound, http.StatusNotFound, "not_found", "resource was not found")
	})
	return mux
}

func (h *handler) createUpdate(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	request.Body = http.MaxBytesReader(writer, request.Body, h.maxUpload+(2<<20))
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeServiceError(writer, request, service.ErrTooLarge)
		} else {
			writeServiceError(writer, request, service.ErrInvalid)
		}
		return
	}
	defer request.MultipartForm.RemoveAll()
	content, header, err := request.FormFile("content")
	if err != nil {
		writeServiceError(writer, request, service.ErrInvalid)
		return
	}
	defer content.Close()
	session, err := h.updates.Create(request.Context(), actor.User, request.PathValue("fileId"), key, header.Filename, content)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, session)
}

func (h *handler) diffUpdate(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	result, err := h.updates.Diff(request.Context(), actor.User, request.PathValue("fileId"), request.PathValue("sessionId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (h *handler) resolveUpdate(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	version, err := h.updates.Resolve(request.Context(), actor.User, request.PathValue("fileId"), request.PathValue("sessionId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, version)
}

func (h *handler) rejectUpdate(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	session, err := h.updates.Reject(request.Context(), actor.User, request.PathValue("fileId"), request.PathValue("sessionId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}

func (h *handler) getLock(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	lock, err := h.locks.Get(request.Context(), actor.User, request.PathValue("fileId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, lock)
}

func (h *handler) createLock(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	lock, err := h.locks.Create(request.Context(), actor.User, request.PathValue("fileId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, lock)
}

func (h *handler) releaseLock(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	if _, err := h.locks.Release(request.Context(), actor.User, request.PathValue("fileId")); err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *handler) listLinks(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	page, err := h.links.List(request.Context(), actor.User, request.PathValue("fileId"), request.URL.Query().Get("cursor"), queryInt(request, "limit"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (h *handler) revokeLink(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	if err := h.links.Revoke(request.Context(), actor.User, request.PathValue("fileId"), request.PathValue("linkId")); err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *handler) downloadLink(writer http.ResponseWriter, request *http.Request) {
	var user *domain.User
	if request.Header.Get("Authorization") != "" {
		actor, _, ok := h.authenticate(writer, request)
		if !ok {
			return
		}
		user = &actor.User
	}
	metadata, object, err := h.links.Download(request.Context(), user, request.PathValue("token"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	defer object.Body.Close()
	writer.Header().Set("Content-Type", metadata.MIMEType)
	writer.Header().Set("Content-Length", strconv.FormatInt(metadata.Size, 10))
	writer.Header().Set("ETag", `"`+metadata.SHA256+`"`)
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": metadata.OriginalName}))
	writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(writer, object.Body)
}

func (h *handler) register(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(writer, request, &body) {
		return
	}
	result, err := h.identity.Register(request.Context(), body.Name, body.Email, body.Password)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (h *handler) login(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(writer, request, &body) {
		return
	}
	result, err := h.identity.Login(request.Context(), body.Email, body.Password)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (h *handler) logout(writer http.ResponseWriter, request *http.Request) {
	_, rawToken, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	if err := h.identity.Logout(request.Context(), rawToken); err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *handler) me(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if ok {
		writeJSON(writer, http.StatusOK, actor.User)
	}
}

func (h *handler) baseWorkspace(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	workspace, err := h.workspaces.Base(request.Context(), actor.User)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, workspace)
}

func (h *handler) listWorkspaces(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	workspaces, err := h.workspaces.List(request.Context(), actor.User)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, workspaces)
}

func (h *handler) createWorkspace(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(writer, request, &body) {
		return
	}
	workspace, err := h.workspaces.Create(request.Context(), actor.User, body.Name)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, workspace)
}

func (h *handler) getWorkspace(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	workspace, err := h.workspaces.Get(request.Context(), actor.User, request.PathValue("workspaceId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, workspace)
}

func (h *handler) putWorkspaceMemberByID(writer http.ResponseWriter, request *http.Request) {
	h.putWorkspaceMember(writer, request, request.PathValue("userId"), "")
}

func (h *handler) putWorkspaceMemberByEmail(writer http.ResponseWriter, request *http.Request) {
	h.putWorkspaceMember(writer, request, "", "email")
}

func (h *handler) putWorkspaceMember(writer http.ResponseWriter, request *http.Request, userID, emailMode string) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	var body struct {
		Email string               `json:"email"`
		Role  domain.WorkspaceRole `json:"role"`
	}
	if !decodeJSON(writer, request, &body) {
		return
	}
	if emailMode == "" && body.Email != "" {
		writeServiceError(writer, request, service.ErrInvalid)
		return
	}
	member, err := h.workspaces.PutMember(request.Context(), actor.User, request.PathValue("workspaceId"), userID, body.Email, body.Role)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, member)
}

func (h *handler) removeWorkspaceMember(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	if err := h.workspaces.RemoveMember(request.Context(), actor.User, request.PathValue("workspaceId"), request.PathValue("userId")); err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *handler) createFile(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, h.maxUpload+(2<<20))
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeServiceError(writer, request, service.ErrTooLarge)
		} else {
			writeServiceError(writer, request, service.ErrInvalid)
		}
		return
	}
	defer request.MultipartForm.RemoveAll()
	content, header, err := request.FormFile("content")
	if err != nil {
		writeServiceError(writer, request, service.ErrInvalid)
		return
	}
	defer content.Close()
	file, err := h.files.Create(request.Context(), actor.User, request.PathValue("workspaceId"), request.FormValue("name"), request.FormValue("text_encoding"), header.Filename, content)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, file)
}

func (h *handler) listFiles(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	page, err := h.files.List(request.Context(), actor.User, request.PathValue("workspaceId"), request.URL.Query().Get("cursor"), queryInt(request, "limit"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (h *handler) getFile(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	file, err := h.files.Get(request.Context(), actor.User, request.PathValue("fileId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, file)
}

func (h *handler) getFileEncoding(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	file, err := h.files.Get(request.Context(), actor.User, request.PathValue("fileId"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"text_encoding": file.TextEncoding})
}

func (h *handler) setFileEncoding(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	var body struct {
		TextEncoding string `json:"text_encoding"`
	}
	if !decodeJSON(writer, request, &body) {
		return
	}
	file, err := h.files.SetEncoding(request.Context(), actor.User, request.PathValue("fileId"), body.TextEncoding)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, file)
}

func (h *handler) fileHistory(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	page, err := h.files.History(request.Context(), actor.User, request.PathValue("fileId"), request.URL.Query().Get("cursor"), queryInt(request, "limit"))
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (h *handler) downloadFile(writer http.ResponseWriter, request *http.Request) {
	actor, _, ok := h.authenticate(writer, request)
	if !ok {
		return
	}
	version := queryInt(request, "version")
	metadata, object, err := h.files.Download(request.Context(), actor.User, request.PathValue("fileId"), version)
	if err != nil {
		writeServiceError(writer, request, err)
		return
	}
	defer object.Body.Close()
	writer.Header().Set("Content-Type", metadata.MIMEType)
	writer.Header().Set("Content-Length", strconv.FormatInt(metadata.Size, 10))
	writer.Header().Set("ETag", `"`+metadata.SHA256+`"`)
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": metadata.OriginalName}))
	writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(writer, object.Body)
}

func (h *handler) authenticate(writer http.ResponseWriter, request *http.Request) (domain.Actor, string, bool) {
	header := request.Header.Get("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		writeServiceError(writer, request, service.ErrUnauthorized)
		return domain.Actor{}, "", false
	}
	rawToken := strings.TrimSpace(parts[1])
	actor, err := h.identity.Authenticate(request.Context(), rawToken)
	if err != nil {
		writeServiceError(writer, request, err)
		return domain.Actor{}, "", false
	}
	return actor, rawToken, true
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxJSONBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(writer, request, err, http.StatusBadRequest, "invalid_request", "request body must be a valid JSON object")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, request, err, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return false
	}
	return true
}

func writeServiceError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalid):
		writeError(writer, request, err, http.StatusBadRequest, "invalid_request", "request validation failed")
	case errors.Is(err, service.ErrUnauthorized):
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeError(writer, request, err, http.StatusUnauthorized, "unauthorized", "valid authentication is required")
	case errors.Is(err, service.ErrForbidden):
		writeError(writer, request, err, http.StatusForbidden, "forbidden", "operation is not allowed")
	case errors.Is(err, service.ErrNotFound):
		writeError(writer, request, err, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, service.ErrConflict):
		writeError(writer, request, err, http.StatusConflict, "conflict", "resource conflicts with current state")
	case errors.Is(err, service.ErrTooLarge):
		writeError(writer, request, err, http.StatusRequestEntityTooLarge, "payload_too_large", "payload exceeds the configured limit")
	case errors.Is(err, service.ErrLocked):
		writeError(writer, request, err, http.StatusLocked, "locked", "resource is locked for writes")
	default:
		writeError(writer, request, err, http.StatusInternalServerError, "internal_error", "request could not be completed")
	}
}

func queryInt(request *http.Request, name string) int {
	value, _ := strconv.Atoi(request.URL.Query().Get(name))
	return value
}

func writeError(writer http.ResponseWriter, request *http.Request, _ error, status int, code, detail string) {
	details := problem.New(status, code, detail)
	details.Instance = request.URL.Path
	if strings.HasPrefix(details.Instance, "/links/") {
		details.Instance = "/links/{token}/content"
	}
	problem.Write(writer, details)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func health(status string) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, healthResponse{Status: status})
	}
}
