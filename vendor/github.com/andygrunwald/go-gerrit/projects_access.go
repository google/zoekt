package gerrit

import (
	"fmt"
	"net/url"
)

// ProjectAccessInput describes changes that should be applied to a project access config
//
// Gerrit API docs: https://gerrit-review.googlesource.com/Documentation/rest-api-projects.html#project-access-input
type ProjectAccessInput struct {
	// A list of deductions to be applied to the project access as ProjectAccessInfo entities.
	Remove map[string]AccessSectionInfo `json:"remove"`

	// A list of additions to be applied to the project access as ProjectAccessInfo entities.
	Add map[string]AccessSectionInfo `json:"add"`

	// A commit message for this change.
	Message string `json:"message"`

	// A new parent for the project to inherit from. Changing the parent project requires administrative privileges.
	Parent string `json:"parent"`
}

// AccessCheckInfo entity is the result of an access check.
//
// Gerrit API docs: https://gerrit-review.googlesource.com/Documentation/rest-api-projects.html#access-check-info
type AccessCheckInfo struct {
	// The HTTP status code for the access. 200 means success and 403 means denied.
	Status int `json:"status"`

	// A clarifying message if status is not 200.
	Message string `json:"message"`
}

// CheckAccessOptions is options for check access
type CheckAccessOptions struct {
	// The account for which to check access. Mandatory.
	Account string `url:"account,omitempty"`

	// The ref permission for which to check access. If not specified, read access to at least branch is checked.
	Permission string `url:"perm,omitempty"`

	// The branch for which to check access. This must be given if perm is specified.
	Ref string `url:"ref,omitempty"`
}

// ListAccessRights lists the access rights for a single project
//
// Gerrit API docs: https://gerrit-review.googlesource.com/Documentation/rest-api-projects.html#get-access
func (s *ProjectsService) ListAccessRights(projectName string) (*ProjectAccessInfo, *Response, error) {
	u := fmt.Sprintf("projects/%s/access", url.QueryEscape(projectName))

	req, err := s.client.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	v := new(ProjectAccessInfo)
	resp, err := s.client.Do(req, v)
	if err != nil {
		return nil, resp, err
	}

	return v, resp, err
}

// AddUpdateDeleteAccessRights add, update and delete access rights for project
//
// Sets access rights for the project using the diff schema provided by ProjectAccessInput.
// Deductions are used to remove access sections, permissions or permission rules.
// The backend will remove the entity with the finest granularity in the request,
// meaning that if an access section without permissions is posted, the access section will be removed;
// if an access section with a permission but no permission rules is posted, the permission will be removed;
// if an access section with a permission and a permission rule is posted, the permission rule will be removed.
//
// Additionally, access sections and permissions will be cleaned up after applying the deductions by
// removing items that have no child elements.
//
// After removals have been applied, additions will be applied.
//
// Gerrit API docs: https://gerrit-review.googlesource.com/Documentation/rest-api-projects.html#set-access
func (s *ProjectsService) AddUpdateDeleteAccessRights(projectName string, input *ProjectAccessInput) (*ProjectAccessInfo, *Response, error) {
	u := fmt.Sprintf("projects/%s/access", url.QueryEscape(projectName))

	req, err := s.client.NewRequest("POST", u, input)
	if err != nil {
		return nil, nil, err
	}

	v := new(ProjectAccessInfo)
	resp, err := s.client.Do(req, v)
	if err != nil {
		return nil, resp, err
	}

	return v, resp, err
}

// CreateAccessRightChange sets access rights for the project using the diff schema provided by ProjectAccessInput
//
// This takes the same input as Update Access Rights, but creates a pending change for review.
// Like Create Change, it returns a ChangeInfo entity describing the resulting change.
//
// Gerrit API docs: https://gerrit-review.googlesource.com/Documentation/rest-api-projects.html#create-access-change
func (s *ProjectsService) CreateAccessRightChange(projectName string, input *ProjectAccessInput) (*ChangeInfo, *Response, error) {
	u := fmt.Sprintf("projects/%s/access:review", url.QueryEscape(projectName))

	req, err := s.client.NewRequest("PUT", u, input)
	if err != nil {
		return nil, nil, err
	}

	v := new(ChangeInfo)
	resp, err := s.client.Do(req, v)
	if err != nil {
		return nil, resp, err
	}

	return v, resp, err
}

// CheckAccess runs access checks for other users. This requires the View Access global capability.
//
// The result is a AccessCheckInfo entity detailing the access of the given user for the given project, project-ref, or project-permission-ref combination.
//
// Gerrit API docs: https://gerrit-review.googlesource.com/Documentation/rest-api-projects.html#check-access
func (s *ProjectsService) CheckAccess(projectName string, opt *CheckAccessOptions) (*AccessCheckInfo, *Response, error) {
	u := fmt.Sprintf("projects/%s/check.access", url.QueryEscape(projectName))

	u, err := addOptions(u, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	v := new(AccessCheckInfo)
	resp, err := s.client.Do(req, v)
	if err != nil {
		return nil, resp, err
	}

	return v, resp, err
}
