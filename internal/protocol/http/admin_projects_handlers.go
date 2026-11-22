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
