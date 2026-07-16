package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexonderia/filestore/internal/domain"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type APIError struct {
	Status int
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (err *APIError) Error() string {
	if err.Code == "" {
		return fmt.Sprintf("API returned HTTP %d", err.Status)
	}
	return fmt.Sprintf("API returned %s (HTTP %d): %s", err.Code, err.Status, err.Detail)
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (client *Client) Register(ctx context.Context, name, email, password string) (domain.AuthResult, error) {
	var result domain.AuthResult
	err := client.request(ctx, http.MethodPost, "/auth/register", map[string]string{"name": name, "email": email, "password": password}, &result)
	return result, err
}

func (client *Client) Login(ctx context.Context, email, password string) (domain.AuthResult, error) {
	var result domain.AuthResult
	err := client.request(ctx, http.MethodPost, "/auth/login", map[string]string{"email": email, "password": password}, &result)
	return result, err
}

func (client *Client) Logout(ctx context.Context) error {
	return client.request(ctx, http.MethodPost, "/auth/logout", nil, nil)
}

func (client *Client) Me(ctx context.Context) (domain.User, error) {
	var result domain.User
	err := client.request(ctx, http.MethodGet, "/auth/me", nil, &result)
	return result, err
}

func (client *Client) BaseWorkspace(ctx context.Context) (domain.Workspace, error) {
	var result domain.Workspace
	err := client.request(ctx, http.MethodGet, "/workspaces/base", nil, &result)
	return result, err
}

func (client *Client) Workspaces(ctx context.Context) ([]domain.Workspace, error) {
	var result []domain.Workspace
	err := client.request(ctx, http.MethodGet, "/workspaces", nil, &result)
	return result, err
}

func (client *Client) CreateWorkspace(ctx context.Context, name string) (domain.Workspace, error) {
	var result domain.Workspace
	err := client.request(ctx, http.MethodPost, "/workspaces", map[string]string{"name": name}, &result)
	return result, err
}

func (client *Client) Workspace(ctx context.Context, workspaceID string) (domain.Workspace, error) {
	var result domain.Workspace
	err := client.request(ctx, http.MethodGet, "/workspaces/"+url.PathEscape(workspaceID), nil, &result)
	return result, err
}

func (client *Client) PutMember(ctx context.Context, workspaceID, email string, role domain.WorkspaceRole) (domain.WorkspaceMember, error) {
	var result domain.WorkspaceMember
	err := client.request(ctx, http.MethodPost, "/workspaces/"+url.PathEscape(workspaceID)+"/members", map[string]string{"email": email, "role": string(role)}, &result)
	return result, err
}

func (client *Client) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	return client.request(ctx, http.MethodDelete, "/workspaces/"+url.PathEscape(workspaceID)+"/members/"+url.PathEscape(userID), nil, nil)
}

func (client *Client) Upload(ctx context.Context, workspaceID, name, encoding, originalName string, source io.Reader) (domain.File, error) {
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	go func() {
		var err error
		if name != "" {
			err = multipartWriter.WriteField("name", name)
		}
		if err == nil && encoding != "" {
			err = multipartWriter.WriteField("text_encoding", encoding)
		}
		if err == nil {
			var part io.Writer
			part, err = multipartWriter.CreateFormFile("content", originalName)
			if err == nil {
				_, err = io.Copy(part, source)
			}
		}
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		_ = pipeWriter.CloseWithError(err)
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/workspaces/"+url.PathEscape(workspaceID)+"/files", pipeReader)
	if err != nil {
		return domain.File{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	var result domain.File
	err = client.do(request, &result)
	return result, err
}

func (client *Client) Files(ctx context.Context, workspaceID string) (domain.FilePage, error) {
	return client.FilesPage(ctx, workspaceID, "", 0)
}

func (client *Client) FilesPage(ctx context.Context, workspaceID, cursor string, limit int) (domain.FilePage, error) {
	var result domain.FilePage
	err := client.request(ctx, http.MethodGet, "/workspaces/"+url.PathEscape(workspaceID)+"/files"+pageQuery(cursor, limit), nil, &result)
	return result, err
}

func (client *Client) File(ctx context.Context, fileID string) (domain.File, error) {
	var result domain.File
	err := client.request(ctx, http.MethodGet, "/files/"+url.PathEscape(fileID), nil, &result)
	return result, err
}

func (client *Client) History(ctx context.Context, fileID string) (domain.VersionPage, error) {
	return client.HistoryPage(ctx, fileID, "", 0)
}

func (client *Client) HistoryPage(ctx context.Context, fileID, cursor string, limit int) (domain.VersionPage, error) {
	var result domain.VersionPage
	err := client.request(ctx, http.MethodGet, "/files/"+url.PathEscape(fileID)+"/versions"+pageQuery(cursor, limit), nil, &result)
	return result, err
}

func (client *Client) SetEncoding(ctx context.Context, fileID, encoding string) (domain.File, error) {
	var result domain.File
	err := client.request(ctx, http.MethodPatch, "/files/"+url.PathEscape(fileID)+"/encoding", map[string]string{"text_encoding": encoding}, &result)
	return result, err
}

func (client *Client) Download(ctx context.Context, fileID string, version int, destination io.Writer) error {
	path := "/files/" + url.PathEscape(fileID) + "/content"
	if version > 0 {
		path += "?version=" + fmt.Sprint(version)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return err
	}
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return decodeAPIError(response)
	}
	_, err = io.Copy(destination, response.Body)
	return err
}

func (client *Client) CreateUpdate(ctx context.Context, fileID, key, originalName string, source io.Reader) (domain.UpdateSession, error) {
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	go func() {
		part, err := multipartWriter.CreateFormFile("content", originalName)
		if err == nil {
			_, err = io.Copy(part, source)
		}
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		_ = pipeWriter.CloseWithError(err)
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/files/"+url.PathEscape(fileID)+"/updates", pipeReader)
	if err != nil {
		return domain.UpdateSession{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("Idempotency-Key", key)
	var result domain.UpdateSession
	err = client.do(request, &result)
	return result, err
}

func (client *Client) UpdateDiff(ctx context.Context, fileID, sessionID string) (domain.DiffResult, error) {
	var result domain.DiffResult
	err := client.request(ctx, http.MethodGet, "/files/"+url.PathEscape(fileID)+"/updates/"+url.PathEscape(sessionID)+"/diff", nil, &result)
	return result, err
}

func (client *Client) ResolveUpdate(ctx context.Context, fileID, sessionID string) (domain.FileVersion, error) {
	var result domain.FileVersion
	err := client.request(ctx, http.MethodPost, "/files/"+url.PathEscape(fileID)+"/updates/"+url.PathEscape(sessionID)+"/resolve", nil, &result)
	return result, err
}

func (client *Client) RejectUpdate(ctx context.Context, fileID, sessionID string) (domain.UpdateSession, error) {
	var result domain.UpdateSession
	err := client.request(ctx, http.MethodPost, "/files/"+url.PathEscape(fileID)+"/updates/"+url.PathEscape(sessionID)+"/reject", nil, &result)
	return result, err
}

func (client *Client) Lock(ctx context.Context, fileID string) (domain.FileLock, error) {
	var result domain.FileLock
	err := client.request(ctx, http.MethodPost, "/files/"+url.PathEscape(fileID)+"/lock", nil, &result)
	return result, err
}

func (client *Client) LockStatus(ctx context.Context, fileID string) (domain.FileLock, error) {
	var result domain.FileLock
	err := client.request(ctx, http.MethodGet, "/files/"+url.PathEscape(fileID)+"/lock", nil, &result)
	return result, err
}

func (client *Client) Unlock(ctx context.Context, fileID string) error {
	return client.request(ctx, http.MethodDelete, "/files/"+url.PathEscape(fileID)+"/lock", nil, nil)
}

func (client *Client) Links(ctx context.Context, fileID string) (domain.LinkPage, error) {
	return client.LinksPage(ctx, fileID, "", 0)
}

func (client *Client) LinksPage(ctx context.Context, fileID, cursor string, limit int) (domain.LinkPage, error) {
	var result domain.LinkPage
	err := client.request(ctx, http.MethodGet, "/files/"+url.PathEscape(fileID)+"/links"+pageQuery(cursor, limit), nil, &result)
	return result, err
}

func pageQuery(cursor string, limit int) string {
	values := url.Values{}
	if cursor != "" {
		values.Set("cursor", cursor)
	}
	if limit > 0 {
		values.Set("limit", fmt.Sprint(limit))
	}
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

func (client *Client) RevokeLink(ctx context.Context, fileID, linkID string) error {
	return client.request(ctx, http.MethodDelete, "/files/"+url.PathEscape(fileID)+"/links/"+url.PathEscape(linkID), nil, nil)
}

func (client *Client) DownloadLink(ctx context.Context, token string, destination io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/links/"+url.PathEscape(token)+"/content", nil)
	if err != nil {
		return err
	}
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return decodeAPIError(response)
	}
	_, err = io.Copy(destination, response.Body)
	return err
}

func (client *Client) request(ctx context.Context, method, path string, body, result any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json, application/problem+json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	return client.do(request, result)
}

func (client *Client) do(request *http.Request, result any) error {
	request.Header.Set("Accept", "application/json, application/problem+json")
	if client.token != "" && request.Header.Get("Authorization") == "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return decodeAPIError(response)
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	return nil
}

func decodeAPIError(response *http.Response) error {
	apiError := &APIError{Status: response.StatusCode}
	_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(apiError)
	return apiError
}
