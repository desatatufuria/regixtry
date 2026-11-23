package regixtryhttp

import (
	"strings"
	"time"

	stdhttp "net/http"
	domainauth "regixtry/internal/domain/auth"
	domainregistry "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// adminProjectResponse is the JSON shape of one project: its identity and its
// derived totals. last_pushed is null when nothing was ever pushed.
type adminProjectResponse struct {
	Name         string     `json:"name"`
	CreatedAt    time.Time  `json:"created_at"`
	CreatedBy    string     `json:"created_by"`
	Repositories int        `json:"repositories"`
	Tags         int        `json:"tags"`
	LastPushed   *time.Time `json:"last_pushed"`
}

func newAdminProjectResponse(project ports.ProjectSummary) adminProjectResponse {
	response := adminProjectResponse{
		Name:         project.Name,
		CreatedAt:    project.CreatedAt,
		CreatedBy:    project.CreatedBy,
		Repositories: project.Repositories,
		Tags:         project.Tags,
	}
	if !project.LastPushed.IsZero() {
		lastPushed := project.LastPushed
		response.LastPushed = &lastPushed
	}

	return response
}

// WithProjects installs the project service behind /admin/v1/projects. Without
// it those routes answer 501 (projects are not available in this deployment);
// the member routes live in the auth service and answer likewise when it has
// no project directory.
func WithProjects(projects ports.ProjectAdmin) RouterOption {
	return func(router *Router) {
		router.projects = projects
	}
}

// adminProjectMembersPath recognizes the delegate-eligible member routes of
// the already-trimmed admin subpath: projects/{project}/members and
// projects/{project}/members/{user}. Every other projects/... path stays under
// requireAdminPrincipal by construction, like the rest of /admin/v1.
func adminProjectMembersPath(subpath string) (project string, username string, ok bool) {
	rest, found := strings.CutPrefix(subpath, "projects/")
	if !found {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 2 && parts[1] == "members":
		return parts[0], "", parts[0] != ""
	case len(parts) == 3 && parts[1] == "members":
		return parts[0], parts[2], parts[0] != "" && parts[2] != ""
	}

	return "", "", false
}

// handleAdminProjects serves the global-administrator project routes:
// GET and POST projects, GET and DELETE projects/{project}. The project name
// is validated by the project service (ParseProjectName: lowercase, one
// segment, not reserved).
func (r *Router) handleAdminProjects(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, resource string) {
	if r.projects == nil {
		writeAdminError(w, domainregistry.NewUnsupportedError("projects are not available in this deployment"), ports.Challenge{})
		return
	}
	ctx := ports.ContextWithPrincipal(req.Context(), principal)

	if resource == "" {
		switch req.Method {
		case stdhttp.MethodGet:
			projects, err := r.projects.ListProjects(ctx)
			if err != nil {
				writeAdminError(w, err, ports.Challenge{})
				return
			}
			response := make([]adminProjectResponse, 0, len(projects))
			for _, project := range projects {
				response = append(response, newAdminProjectResponse(project))
			}
			writeJSON(w, stdhttp.StatusOK, map[string]any{"projects": response})
		case stdhttp.MethodPost:
			var input struct {
				Name string `json:"name"`
			}
			if err := decodeAdminJSON(req, &input); err != nil {
				writeAdminError(w, err, ports.Challenge{})
				return
			}
			if err := r.projects.CreateProject(ctx, input.Name); err != nil {
				writeAdminError(w, err, ports.Challenge{})
				return
			}
			project, err := r.projects.GetProject(ctx, input.Name)
			if err != nil {
				writeAdminError(w, err, ports.Challenge{})
				return
			}
			writeJSON(w, stdhttp.StatusCreated, newAdminProjectResponse(project))
		default:
			w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		}
		return
	}

	if strings.Contains(resource, "/") {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}
	switch req.Method {
	case stdhttp.MethodGet:
		project, err := r.projects.GetProject(ctx, resource)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, newAdminProjectResponse(project))
	case stdhttp.MethodDelete:
		if err := r.projects.DeleteProject(ctx, resource); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]string{"deleted": resource})
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminProjectMembers serves the delegate-eligible member routes. The
// caller is authenticated but not required to be a global admin: the auth
// service decides (global admin, or a project admin of exactly this project)
// and refuses before it says anything about the project's existence.
func (r *Router) handleAdminProjectMembers(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, project string, username string) {
	if username == "" {
		if req.Method != stdhttp.MethodGet {
			w.Header().Set("Allow", stdhttp.MethodGet)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}

		limit := adminAllGrantsDefaultLimit
		query := req.URL.Query()
		if query.Has("limit") {
			var err error
			limit, err = parseAdminLimit(query.Get("limit"))
			if err != nil {
				writeAdminError(w, err, ports.Challenge{})
				return
			}
		}
		page, err := r.admin.ListAdminProjectMembers(req.Context(), principal, project, limit, query.Get("after"))
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		if page.Members == nil {
			page.Members = []ports.AdminProjectMember{}
		}
		writeJSON(w, stdhttp.StatusOK, page)
		return
	}

	switch req.Method {
	case stdhttp.MethodPut:
		var input ports.AdminPutProjectGrantInput
		if err := decodeAdminJSON(req, &input); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		input.Project = project
		input.Username = username

		member, err := r.admin.PutAdminProjectGrant(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, member)
	case stdhttp.MethodDelete:
		if err := r.admin.DeleteAdminProjectGrant(req.Context(), principal, project, username); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodPut, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminUserProjectGrants serves GET /admin/v1/users/{user}/project-grants
// (global administrators only, like the repository grants of a user).
func (r *Router) handleAdminUserProjectGrants(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	grants, err := r.admin.ListAdminUserProjectGrants(req.Context(), principal, userID)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	writeJSON(w, stdhttp.StatusOK, grants)
}
