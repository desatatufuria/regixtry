package tui

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"regixtry/internal/ports"
)

// adminProjectMembersMaxLimit is the most rows GET /admin/v1/projects/{p}/members
// returns per page; a larger request is a server validation error, so it is
// clamped.
const adminProjectMembersMaxLimit = 200

// adminProjectWire is the JSON shape of one project (docs/api.md): last_pushed
// is null when nothing was ever pushed.
type adminProjectWire struct {
	Name         string     `json:"name"`
	CreatedAt    time.Time  `json:"created_at"`
	CreatedBy    string     `json:"created_by"`
	Repositories int        `json:"repositories"`
	Tags         int        `json:"tags"`
	LastPushed   *time.Time `json:"last_pushed"`
}

func (w adminProjectWire) summary() ports.ProjectSummary {
	summary := ports.ProjectSummary{Name: w.Name, CreatedAt: w.CreatedAt, CreatedBy: w.CreatedBy, Repositories: w.Repositories, Tags: w.Tags}
	if w.LastPushed != nil {
		summary.LastPushed = *w.LastPushed
	}
	return summary
}

func projectPath(project string) string {
	return "/admin/v1/projects/" + url.PathEscape(strings.TrimSpace(project))
}

func projectMemberPath(project string, username string) string {
	return projectPath(project) + "/members/" + url.PathEscape(strings.TrimSpace(username))
}

func (c *HTTPAdminClient) ListProjects(ctx context.Context, session AdminSession) ([]ports.ProjectSummary, error) {
	var payload struct {
		Projects []adminProjectWire `json:"projects"`
	}
	if err := c.getJSON(ctx, session, "/admin/v1/projects", &payload); err != nil {
		return nil, err
	}
	projects := make([]ports.ProjectSummary, 0, len(payload.Projects))
	for _, wire := range payload.Projects {
		projects = append(projects, wire.summary())
	}
	return projects, nil
}

func (c *HTTPAdminClient) CreateProject(ctx context.Context, session AdminSession, name string) (ports.ProjectSummary, error) {
	var wire adminProjectWire
	body := map[string]string{"name": name}
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, "/admin/v1/projects", body, &wire, stdhttp.StatusCreated); err != nil {
		return ports.ProjectSummary{}, err
	}
	return wire.summary(), nil
}

func (c *HTTPAdminClient) GetProject(ctx context.Context, session AdminSession, name string) (ports.ProjectSummary, error) {
	var wire adminProjectWire
	if err := c.getJSON(ctx, session, projectPath(name), &wire); err != nil {
		return ports.ProjectSummary{}, err
	}
	return wire.summary(), nil
}

func (c *HTTPAdminClient) DeleteProject(ctx context.Context, session AdminSession, name string) error {
	return c.requestJSON(ctx, stdhttp.MethodDelete, session, projectPath(name), nil, nil, stdhttp.StatusOK)
}

func (c *HTTPAdminClient) ListProjectMembers(ctx context.Context, session AdminSession, project string, limit int, after string) (ports.AdminProjectMemberPage, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(min(limit, adminProjectMembersMaxLimit)))
	}
	if after != "" {
		query.Set("after", after)
	}
	path := projectPath(project) + "/members"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var page ports.AdminProjectMemberPage
	if err := c.getJSON(ctx, session, path, &page); err != nil {
		return ports.AdminProjectMemberPage{}, err
	}
	return page, nil
}

func (c *HTTPAdminClient) PutProjectMember(ctx context.Context, session AdminSession, input ports.AdminPutProjectGrantInput) (ports.AdminProjectMember, error) {
	var member ports.AdminProjectMember
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, projectMemberPath(input.Project, input.Username), input, &member, stdhttp.StatusOK); err != nil {
		return ports.AdminProjectMember{}, err
	}
	return member, nil
}

func (c *HTTPAdminClient) DeleteProjectMember(ctx context.Context, session AdminSession, project string, username string) error {
	return c.requestNoContent(ctx, stdhttp.MethodDelete, session, projectMemberPath(project, username), nil, stdhttp.StatusNoContent)
}

func (c *HTTPAdminClient) ListUserProjectGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminProjectGrant, error) {
	var grants []ports.AdminProjectGrant
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(userID)) + "/project-grants"
	if err := c.getJSON(ctx, session, path, &grants); err != nil {
		return nil, err
	}
	return grants, nil
}
