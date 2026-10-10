package pipeline

import "github.com/google/uuid"

func NewRunID() string {
	return uuid.NewString()
}
