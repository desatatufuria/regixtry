package regixtry

import "time"

type UploadStatus string

const (
	UploadStatusActive    UploadStatus = "active"
	UploadStatusCommitted UploadStatus = "committed"
	UploadStatusCanceled  UploadStatus = "canceled"
)

type UploadState struct {
	ID         string
	Repository RepositoryRef
	Status     UploadStatus
	Size       int64
	StartedAt  time.Time
	UpdatedAt  time.Time
	Location   string
}

func (u UploadState) Validate() error {
	if u.ID == "" {
		return NewValidationError("upload ID is required")
	}

	if err := u.Repository.Validate(); err != nil {
		return err
	}

	if u.Status == "" {
		return NewValidationError("upload status is required")
	}

	if u.Size < 0 {
		return NewValidationError("upload size must be zero or positive")
	}

	if u.StartedAt.IsZero() || u.UpdatedAt.IsZero() {
		return NewValidationError("upload timestamps are required")
	}

	return nil
}
