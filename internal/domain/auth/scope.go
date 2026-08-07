package auth

import (
	"sort"
	"strings"

	regixtrydomain "regixtry/internal/domain/regixtry"
)

const (
	scopeTypeRepository = "repository"
	scopeTypeRegixtry   = "regixtry"
	actionPull          = "pull"
	actionPush          = "push"
	actionCatalog       = "catalog"
	actionWildcard      = "*"
)

type Scope struct {
	Type      string
	Name      string
	Actions   []string
	Canonical string

	repository regixtrydomain.RepositoryRef
}

func ParseScopes(values []string) ([]Scope, error) {
	parsed := make([]Scope, 0)
	for _, value := range values {
		for _, candidate := range strings.Fields(strings.TrimSpace(value)) {
			if candidate == "" {
				continue
			}

			scope, err := ParseScope(candidate)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, scope)
		}
	}

	return parsed, nil
}

func ParseScope(raw string) (Scope, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 3 {
		return Scope{}, NewValidationError("scope must use the format <type>:<name>:<actions>")
	}

	resourceType := strings.TrimSpace(parts[0])
	resourceName := strings.TrimSpace(parts[1])
	if resourceName == "" {
		return Scope{}, NewValidationError("scope resource name is required")
	}

	switch resourceType {
	case scopeTypeRepository:
		repository, err := regixtrydomain.ParseRepositoryRef(resourceName)
		if err != nil {
			return Scope{}, err
		}

		actions, err := normalizeScopeActions(parts[2], true)
		if err != nil {
			return Scope{}, err
		}

		return Scope{
			Type:       scopeTypeRepository,
			Name:       repository.String(),
			Actions:    actions,
			Canonical:  scopeTypeRepository + ":" + repository.String() + ":" + strings.Join(actions, ","),
			repository: repository,
		}, nil
	case scopeTypeRegixtry:
		if resourceName != actionCatalog {
			return Scope{}, NewValidationError("registry scope name is invalid")
		}

		actions, err := normalizeScopeActions(parts[2], false)
		if err != nil {
			return Scope{}, err
		}

		return Scope{Type: scopeTypeRegixtry, Name: resourceName, Actions: actions, Canonical: scopeTypeRegixtry + ":" + resourceName + ":" + strings.Join(actions, ",")}, nil
	default:
		return Scope{}, NewValidationError("scope resource type is invalid")
	}
}

func NormalizeScopes(scopes []Scope) string {
	if len(scopes) == 0 {
		return ""
	}

	canonical := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope.Canonical == "" {
			continue
		}
		if _, ok := seen[scope.Canonical]; ok {
			continue
		}
		seen[scope.Canonical] = struct{}{}
		canonical = append(canonical, scope.Canonical)
	}

	sort.Strings(canonical)
	return strings.Join(canonical, " ")
}

func (s Scope) IsRepository() bool {
	return s.Type == scopeTypeRepository
}

func (s Scope) IsRegixtryCatalog() bool {
	return s.Type == scopeTypeRegixtry && s.Name == actionCatalog && len(s.Actions) == 1 && s.Actions[0] == actionWildcard
}

func (s Scope) Repository() regixtrydomain.RepositoryRef {
	return s.repository
}

func (s Scope) AllowsPull() bool {
	return s.hasAction(actionPull)
}

func (s Scope) AllowsPush() bool {
	return s.hasAction(actionPush)
}

func (s Scope) hasAction(action string) bool {
	for _, candidate := range s.Actions {
		if candidate == action {
			return true
		}
	}

	return false
}

func normalizeScopeActions(raw string, repository bool) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	if len(parts) == 0 {
		return nil, NewValidationError("scope actions are required")
	}

	seen := map[string]struct{}{}
	actions := make([]string, 0, len(parts))
	for _, part := range parts {
		action := strings.TrimSpace(part)
		if action == "" {
			return nil, NewValidationError("scope actions are required")
		}

		if repository {
			if action != actionPull && action != actionPush {
				return nil, NewValidationError("repository scope actions must be pull and/or push")
			}
		} else if action != actionWildcard {
			return nil, NewValidationError("registry catalog scopes must use *")
		}

		if _, ok := seen[action]; ok {
			continue
		}
		seen[action] = struct{}{}
		actions = append(actions, action)
	}

	if repository {
		sort.Slice(actions, func(i, j int) bool {
			weight := func(action string) int {
				switch action {
				case actionPull:
					return 0
				case actionPush:
					return 1
				default:
					return 2
				}
			}
			return weight(actions[i]) < weight(actions[j])
		})
	} else {
		sort.Strings(actions)
	}

	if len(actions) == 0 {
		return nil, NewValidationError("scope actions are required")
	}

	return actions, nil
}
