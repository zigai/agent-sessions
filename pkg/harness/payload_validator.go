package harness

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

type HookPayloadValidator func(json.RawMessage) bool

type claudeHookPayload struct {
	SessionID      string `json:"session_id"      validate:"required,notblank"`
	TranscriptPath string `json:"transcript_path" validate:"required,pathcontains=/.claude/"`
	CWD            string `json:"cwd"             validate:"required,notblank"`
	HookEventName  string `json:"hook_event_name" validate:"required,notblank"`
}

type codexHookPayload struct {
	SessionID      string  `json:"session_id"      validate:"required,notblank"`
	TranscriptPath *string `json:"transcript_path" validate:"omitempty,pathcontains=/.codex/"`
	CWD            string  `json:"cwd"             validate:"required,notblank"`
	HookEventName  string  `json:"hook_event_name" validate:"required,notblank"`
	Model          string  `json:"model"           validate:"required,notblank"`
}

type cursorHookPayload struct {
	SessionID      string   `json:"session_id"      validate:"required,notblank"`
	TranscriptPath *string  `json:"transcript_path" validate:"omitempty"`
	WorkspaceRoots []string `json:"workspace_roots" validate:"required,min=1,dive,notblank"`
	HookEventName  string   `json:"hook_event_name" validate:"required,notblank"`
	CursorVersion  string   `json:"cursor_version"  validate:"required,notblank"`
}

type grokHookPayload struct {
	SessionID     string `validate:"required,notblank"`
	HookEventName string `validate:"required,notblank"`
	CWD           string `json:"cwd"                   validate:"required,notblank"`
	WorkspaceRoot string `validate:"required,notblank"`
}

type kimiCodeHookPayload struct {
	SessionID     string `json:"session_id"      validate:"required,notblank"`
	CWD           string `json:"cwd"             validate:"required,notblank"`
	HookEventName string `json:"hook_event_name" validate:"required,notblank"`
}

type agyHookPayload struct {
	ConversationID string   `validate:"required,notblank"`
	WorkspacePaths []string `validate:"required,min=1,dive,notblank"`
}

var (
	hookPayloadValidatorOnce sync.Once
	hookPayloadValidate      *validator.Validate
)

func payloadValidator[T any]() HookPayloadValidator {
	return func(rawPayload json.RawMessage) bool {
		var payload T
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return false
		}

		return hookPayloadValidator().Struct(payload) == nil
	}
}

func hookPayloadValidator() *validator.Validate {
	hookPayloadValidatorOnce.Do(func() {
		hookPayloadValidate = validator.New(validator.WithRequiredStructEnabled())
		if err := hookPayloadValidate.RegisterValidation("pathcontains", validatePathContains); err != nil {
			panic(err)
		}
		if err := hookPayloadValidate.RegisterValidation("notblank", validateNotBlank); err != nil {
			panic(err)
		}
	})

	return hookPayloadValidate
}

func validatePathContains(field validator.FieldLevel) bool {
	value, ok := validationString(field.Field())
	if !ok {
		return false
	}

	return strings.Contains(normalizePayloadPath(value), field.Param())
}

func validateNotBlank(field validator.FieldLevel) bool {
	value, ok := validationString(field.Field())
	return ok && value != ""
}

func validationString(value reflect.Value) (string, bool) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.String {
		return "", false
	}

	return strings.TrimSpace(value.String()), true
}

func normalizePayloadPath(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}
