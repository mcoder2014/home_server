package model

import (
	"encoding/json"
	"fmt"
)

// WebProjectAccess is stored as a compact integer. String conversion belongs at
// the API boundary so database indexes never depend on mutable text labels.
type WebProjectAccess uint8

const (
	WebProjectAccessOwner         WebProjectAccess = 1
	WebProjectAccessMembers       WebProjectAccess = 2
	WebProjectAccessAuthenticated WebProjectAccess = 3
	WebProjectAccessPublic        WebProjectAccess = 4
)

func (value WebProjectAccess) String() string {
	switch value {
	case WebProjectAccessOwner:
		return "owner"
	case WebProjectAccessMembers:
		return "members"
	case WebProjectAccessAuthenticated:
		return "authenticated"
	case WebProjectAccessPublic:
		return "public"
	default:
		return ""
	}
}

func (value WebProjectAccess) MarshalJSON() ([]byte, error) {
	if value.String() == "" {
		return nil, fmt.Errorf("unknown web project access value %d", value)
	}
	return json.Marshal(value.String())
}

func ParseWebProjectAccess(value string) (WebProjectAccess, bool) {
	switch value {
	case "owner":
		return WebProjectAccessOwner, true
	case "members":
		return WebProjectAccessMembers, true
	case "authenticated":
		return WebProjectAccessAuthenticated, true
	case "public":
		return WebProjectAccessPublic, true
	default:
		return 0, false
	}
}

// WebProjectStatus is the database state of a project. Values are append-only:
// changing an existing number would reinterpret persisted rows.
type WebProjectStatus uint8

const (
	WebProjectStatusDraft    WebProjectStatus = 1
	WebProjectStatusEnabled  WebProjectStatus = 2
	WebProjectStatusDisabled WebProjectStatus = 3
	WebProjectStatusDeleted  WebProjectStatus = 4
)

func (value WebProjectStatus) String() string {
	switch value {
	case WebProjectStatusDraft:
		return "draft"
	case WebProjectStatusEnabled:
		return "enabled"
	case WebProjectStatusDisabled:
		return "disabled"
	case WebProjectStatusDeleted:
		return "deleted"
	default:
		return ""
	}
}

func (value WebProjectStatus) MarshalJSON() ([]byte, error) {
	if value.String() == "" {
		return nil, fmt.Errorf("unknown web project status value %d", value)
	}
	return json.Marshal(value.String())
}

func ParseWebProjectStatus(value string) (WebProjectStatus, bool) {
	switch value {
	case "draft":
		return WebProjectStatusDraft, true
	case "enabled":
		return WebProjectStatusEnabled, true
	case "disabled":
		return WebProjectStatusDisabled, true
	case "deleted":
		return WebProjectStatusDeleted, true
	default:
		return 0, false
	}
}

// WebProjectReleaseStatus is stored as an integer for bounded filtering and
// indexing while MarshalJSON preserves the existing string HTTP contract.
type WebProjectReleaseStatus uint8

const (
	WebProjectReleaseUploading WebProjectReleaseStatus = 1
	WebProjectReleaseReady     WebProjectReleaseStatus = 2
	WebProjectReleaseFailed    WebProjectReleaseStatus = 3
	WebProjectReleaseDeleting  WebProjectReleaseStatus = 4
)

func (value WebProjectReleaseStatus) String() string {
	switch value {
	case WebProjectReleaseUploading:
		return "uploading"
	case WebProjectReleaseReady:
		return "ready"
	case WebProjectReleaseFailed:
		return "failed"
	case WebProjectReleaseDeleting:
		return "deleting"
	default:
		return ""
	}
}

func (value WebProjectReleaseStatus) MarshalJSON() ([]byte, error) {
	if value.String() == "" {
		return nil, fmt.Errorf("unknown web project release status value %d", value)
	}
	return json.Marshal(value.String())
}

func ParseWebProjectReleaseStatus(value string) (WebProjectReleaseStatus, bool) {
	switch value {
	case "uploading":
		return WebProjectReleaseUploading, true
	case "ready":
		return WebProjectReleaseReady, true
	case "failed":
		return WebProjectReleaseFailed, true
	case "deleting":
		return WebProjectReleaseDeleting, true
	default:
		return 0, false
	}
}
