package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/alexonderia/filestore/internal/domain"
	"github.com/alexonderia/filestore/internal/storage"
	"github.com/pmezard/go-difflib/difflib"
	"golang.org/x/text/encoding/charmap"
)

type UpdateRepository interface {
	ByIdempotency(context.Context, string, string, string) (domain.UpdateSession, error)
	Create(context.Context, string, string, string, string, domain.StoredObject, time.Time) (domain.UpdateSession, error)
	Get(context.Context, string, string) (domain.UpdateSession, error)
	RollbackWarning(context.Context, string, string, string) (bool, error)
	Resolve(context.Context, string, string, string, time.Time) (domain.FileVersion, error)
	Reject(context.Context, string, string, time.Time, string) (string, string, error)
	DeleteStorageRow(context.Context, string) error
	Expired(context.Context, time.Time, int) ([]domain.UpdateSession, error)
	Orphans(context.Context, time.Time, int) ([]domain.StoredObject, error)
}

type Updates struct {
	repository   UpdateRepository
	files        *Files
	objects      storage.ObjectStore
	ttl          time.Duration
	maxSize      int64
	diffMaxInput int64
	diffMaxLines int
	diffMaxOut   int64
	orphanGrace  time.Duration
	now          func() time.Time
}

func NewUpdates(repository UpdateRepository, files *Files, objects storage.ObjectStore, ttl, orphanGrace time.Duration, maxSize, diffInput int64, diffLines int, diffOutput int64) *Updates {
	return &Updates{repository: repository, files: files, objects: objects, ttl: ttl, orphanGrace: orphanGrace, maxSize: maxSize, diffMaxInput: diffInput, diffMaxLines: diffLines, diffMaxOut: diffOutput, now: time.Now}
}

func (service *Updates) Create(ctx context.Context, actor domain.User, fileID, key, originalName string, source io.Reader) (domain.UpdateSession, error) {
	if len(key) < 16 || len(key) > 128 || source == nil {
		return domain.UpdateSession{}, ErrInvalid
	}
	if _, _, _, err := service.files.authorize(ctx, actor, fileID, true); err != nil {
		return domain.UpdateSession{}, err
	}
	if existing, err := service.repository.ByIdempotency(ctx, fileID, actor.ID, key); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return domain.UpdateSession{}, err
	}
	originalName = cleanFilename(originalName)
	if originalName == "" || len(originalName) > 255 {
		return domain.UpdateSession{}, ErrInvalid
	}
	buffered := bufio.NewReaderSize(source, 512)
	header, err := buffered.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return domain.UpdateSession{}, err
	}
	mimeType := http.DetectContentType(header)
	objectKey, err := randomObjectKey("candidates")
	if err != nil {
		return domain.UpdateSession{}, err
	}
	reader := newLimitedHashReader(buffered, service.maxSize)
	if err := service.objects.Put(ctx, objectKey, reader, mimeType); err != nil {
		if errors.Is(reader.err, ErrTooLarge) {
			return domain.UpdateSession{}, ErrTooLarge
		}
		return domain.UpdateSession{}, err
	}
	object := domain.StoredObject{Key: objectKey, Size: reader.size, SHA256: fmt.Sprintf("%x", reader.hash.Sum(nil)), MIMEType: mimeType}
	session, err := service.repository.Create(ctx, fileID, actor.ID, key, originalName, object, service.now().Add(service.ttl))
	if err != nil {
		_ = service.objects.Delete(context.Background(), objectKey)
		return domain.UpdateSession{}, err
	}
	return session, nil
}

func (service *Updates) Diff(ctx context.Context, actor domain.User, fileID, sessionID string) (domain.DiffResult, error) {
	file, _, _, err := service.files.authorize(ctx, actor, fileID, false)
	if err != nil {
		return domain.DiffResult{}, err
	}
	session, err := service.repository.Get(ctx, fileID, sessionID)
	if err != nil || session.Status != "active" {
		if err == nil {
			err = ErrConflict
		}
		return domain.DiffResult{}, err
	}
	result := domain.DiffResult{Kind: "metadata_only", Reason: "binary", Base: session.Base, Candidate: session.Candidate}
	rollback, err := service.repository.RollbackWarning(ctx, fileID, file.CurrentVersion.ID, session.Candidate.SHA256)
	if err != nil {
		return domain.DiffResult{}, err
	}
	result.RollbackWarning = rollback
	baseBytes, baseReason, err := service.readForDiff(ctx, session.BaseKey)
	if err != nil {
		return domain.DiffResult{}, err
	}
	candidateBytes, candidateReason, err := service.readForDiff(ctx, session.CandidateKey)
	if err != nil {
		return domain.DiffResult{}, err
	}
	if baseReason != "" {
		result.Reason = baseReason
		return result, nil
	}
	if candidateReason != "" {
		result.Reason = candidateReason
		return result, nil
	}
	baseText, ok := decodeText(baseBytes, file.TextEncoding)
	if !ok {
		result.Reason = "decode_error"
		return result, nil
	}
	candidateText, ok := decodeText(candidateBytes, file.TextEncoding)
	if !ok {
		result.Reason = "decode_error"
		return result, nil
	}
	baseLines := difflib.SplitLines(baseText)
	candidateLines := difflib.SplitLines(candidateText)
	if len(baseLines) > service.diffMaxLines || len(candidateLines) > service.diffMaxLines {
		result.Reason = "too_many_lines"
		return result, nil
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{A: baseLines, B: candidateLines, FromFile: "base", ToFile: "candidate", Context: 3})
	if err != nil {
		return domain.DiffResult{}, err
	}
	if int64(len(diff)) > service.diffMaxOut {
		result.Reason = "output_too_large"
		return result, nil
	}
	result.Kind, result.Reason, result.UnifiedDiff = "text", "text", diff
	return result, nil
}

func (service *Updates) Resolve(ctx context.Context, actor domain.User, fileID, sessionID string) (domain.FileVersion, error) {
	_, workspace, role, err := service.files.authorize(ctx, actor, fileID, true)
	if err != nil {
		return domain.FileVersion{}, err
	}
	session, err := service.repository.Get(ctx, fileID, sessionID)
	if err != nil {
		return domain.FileVersion{}, err
	}
	owner := role != nil && *role == domain.RoleOwner
	if actor.ID != session.CreatedBy && !owner && !actor.IsSuperadmin && workspace.Kind != domain.WorkspaceBase {
		return domain.FileVersion{}, ErrForbidden
	}
	return service.repository.Resolve(ctx, fileID, sessionID, actor.ID, service.now())
}

func (service *Updates) Reject(ctx context.Context, actor domain.User, fileID, sessionID string) (domain.UpdateSession, error) {
	_, workspace, role, err := service.files.authorize(ctx, actor, fileID, true)
	if err != nil {
		return domain.UpdateSession{}, err
	}
	session, err := service.repository.Get(ctx, fileID, sessionID)
	if err != nil {
		return domain.UpdateSession{}, err
	}
	owner := role != nil && *role == domain.RoleOwner
	if actor.ID != session.CreatedBy && !owner && !actor.IsSuperadmin && workspace.Kind != domain.WorkspaceBase {
		return domain.UpdateSession{}, ErrForbidden
	}
	if err := service.finishCandidate(ctx, fileID, sessionID, "rejected"); err != nil {
		return domain.UpdateSession{}, err
	}
	session.Status, session.CompletedAt, session.CandidateKey = "rejected", service.now(), ""
	return session, nil
}

func (service *Updates) CleanupExpired(ctx context.Context) error {
	sessions, err := service.repository.Expired(ctx, service.now(), 100)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if err := service.finishCandidate(ctx, session.FileID, session.ID, "expired"); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
	}
	orphans, err := service.repository.Orphans(ctx, service.now().Add(-service.orphanGrace), 100)
	if err != nil {
		return err
	}
	for _, object := range orphans {
		if err := service.objects.Delete(ctx, object.Key); err != nil {
			return err
		}
		if err := service.repository.DeleteStorageRow(ctx, object.ID); err != nil {
			return err
		}
	}
	return nil
}

func (service *Updates) finishCandidate(ctx context.Context, fileID, sessionID, status string) error {
	objectID, key, err := service.repository.Reject(ctx, fileID, sessionID, service.now(), status)
	if err != nil {
		return err
	}
	if err := service.objects.Delete(ctx, key); err != nil {
		// The terminal DB transition is authoritative. The orphan reconciler retries cleanup.
		return nil
	}
	// A failed metadata cleanup is also safe to retry as an orphan.
	_ = service.repository.DeleteStorageRow(ctx, objectID)
	return nil
}

func (service *Updates) readForDiff(ctx context.Context, key string) ([]byte, string, error) {
	object, err := service.objects.Get(ctx, key)
	if err != nil {
		return nil, "", err
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, service.diffMaxInput+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > service.diffMaxInput {
		return nil, "input_too_large", nil
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, "binary", nil
	}
	return data, "", nil
}

func decodeText(data []byte, encoding string) (string, bool) {
	switch encoding {
	case "utf-8":
		if !utf8.Valid(data) {
			return "", false
		}
		return string(data), true
	case "windows-1251":
		decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
		return string(decoded), err == nil
	case "utf-16le", "utf-16be":
		if len(data)%2 != 0 {
			return "", false
		}
		words := make([]uint16, len(data)/2)
		for index := range words {
			if encoding == "utf-16le" {
				words[index] = uint16(data[index*2]) | uint16(data[index*2+1])<<8
			} else {
				words[index] = uint16(data[index*2])<<8 | uint16(data[index*2+1])
			}
		}
		return string(utf16.Decode(words)), true
	default:
		return "", false
	}
}
