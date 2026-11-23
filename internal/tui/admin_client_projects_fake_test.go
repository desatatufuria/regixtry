package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"regixtry/internal/ports"
)

// fakeProjectsAPI is the state behind fakeAdminClient's project methods: the
// projects and members the screens read, the errors to inject and the calls
// the fake received, in order.
type fakeProjectsAPI struct {
	projects []ports.ProjectSummary
	// members is every project's members, in the order the server returns them.
	members map[string][]ports.AdminProjectMember
	// userGrants is every user's project roles by user id.
	userGrants map[string][]ports.AdminProjectGrant

	// memberPageSize pages the member list (0 serves everything at once).
	memberPageSize int

	listErr, createErr, getErr, deleteErr  error
	putErr, deleteMemberErr, userGrantsErr error
	membersErr                             map[string]error
	listCalls, getCalls                    int
	createCalls, deleteCalls               []string
	memberReads                            []string // "project|after"
	putCalls                               []ports.AdminPutProjectGrantInput
	deleteMemberCalls                      []string // "project|user"
	userGrantReads                         []string
}

func (f *fakeAdminClient) ListProjects(context.Context, AdminSession) ([]ports.ProjectSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.listCalls++
	if f.projectsAPI.listErr != nil {
		return nil, f.projectsAPI.listErr
	}
	return slices.Clone(f.projectsAPI.projects), nil
}

func (f *fakeAdminClient) CreateProject(_ context.Context, _ AdminSession, name string) (ports.ProjectSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.createCalls = append(f.projectsAPI.createCalls, name)
	if f.projectsAPI.createErr != nil {
		return ports.ProjectSummary{}, f.projectsAPI.createErr
	}
	project := ports.ProjectSummary{Name: name, CreatedAt: fixedTestTime, CreatedBy: "operator"}
	f.projectsAPI.projects = append(f.projectsAPI.projects, project)
	slices.SortFunc(f.projectsAPI.projects, func(a, b ports.ProjectSummary) int { return strings.Compare(a.Name, b.Name) })
	return project, nil
}

func (f *fakeAdminClient) GetProject(_ context.Context, _ AdminSession, name string) (ports.ProjectSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.getCalls++
	if f.projectsAPI.getErr != nil {
		return ports.ProjectSummary{}, f.projectsAPI.getErr
	}
	for _, project := range f.projectsAPI.projects {
		if project.Name == name {
			return project, nil
		}
	}
	return ports.ProjectSummary{}, adminNotFoundError{message: "read admin resource: project not found", detail: "project not found"}
}

func (f *fakeAdminClient) DeleteProject(_ context.Context, _ AdminSession, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.deleteCalls = append(f.projectsAPI.deleteCalls, name)
	if f.projectsAPI.deleteErr != nil {
		return f.projectsAPI.deleteErr
	}
	f.projectsAPI.projects = slices.DeleteFunc(f.projectsAPI.projects, func(p ports.ProjectSummary) bool { return p.Name == name })
	delete(f.projectsAPI.members, name)
	return nil
}

func (f *fakeAdminClient) ListProjectMembers(_ context.Context, _ AdminSession, project string, limit int, after string) (ports.AdminProjectMemberPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.memberReads = append(f.projectsAPI.memberReads, project+"|"+after)
	if err := f.projectsAPI.membersErr[project]; err != nil {
		return ports.AdminProjectMemberPage{}, err
	}
	all := f.projectsAPI.members[project]
	offset := 0
	if after != "" {
		if _, err := fmt.Sscanf(after, "c%d", &offset); err != nil {
			return ports.AdminProjectMemberPage{}, fmt.Errorf("invalid cursor %q", after)
		}
	}
	size := f.projectsAPI.memberPageSize
	if size <= 0 {
		size = len(all) + 1
	}
	end := min(offset+size, len(all))
	page := ports.AdminProjectMemberPage{Members: slices.Clone(all[min(offset, end):end])}
	if end < len(all) {
		page.Next = fmt.Sprintf("c%d", end)
	}
	return page, nil
}

func (f *fakeAdminClient) PutProjectMember(_ context.Context, _ AdminSession, input ports.AdminPutProjectGrantInput) (ports.AdminProjectMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.putCalls = append(f.projectsAPI.putCalls, input)
	if f.projectsAPI.putErr != nil {
		return ports.AdminProjectMember{}, f.projectsAPI.putErr
	}
	if f.projectsAPI.members == nil {
		f.projectsAPI.members = map[string][]ports.AdminProjectMember{}
	}
	member := ports.AdminProjectMember{Username: input.Username, Role: input.Role, CreatedAt: fixedTestTime, UpdatedAt: fixedTestTime}
	list := f.projectsAPI.members[input.Project]
	for i, existing := range list {
		if existing.Username == input.Username {
			member.IsRobot, member.CreatedAt = existing.IsRobot, existing.CreatedAt
			list[i] = member
			return member, nil
		}
	}
	f.projectsAPI.members[input.Project] = append(list, member)
	return member, nil
}

func (f *fakeAdminClient) DeleteProjectMember(_ context.Context, _ AdminSession, project string, username string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.deleteMemberCalls = append(f.projectsAPI.deleteMemberCalls, project+"|"+username)
	if f.projectsAPI.deleteMemberErr != nil {
		return f.projectsAPI.deleteMemberErr
	}
	f.projectsAPI.members[project] = slices.DeleteFunc(f.projectsAPI.members[project], func(m ports.AdminProjectMember) bool { return m.Username == username })
	// The user's own project roles lose it too, so a reload tells the truth.
	for _, user := range f.users {
		if user.Username == username && f.projectsAPI.userGrants != nil {
			f.projectsAPI.userGrants[user.ID] = slices.DeleteFunc(slices.Clone(f.projectsAPI.userGrants[user.ID]), func(g ports.AdminProjectGrant) bool { return g.Project == project })
		}
	}
	return nil
}

func (f *fakeAdminClient) ListUserProjectGrants(_ context.Context, _ AdminSession, userID string) ([]ports.AdminProjectGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsAPI.userGrantReads = append(f.projectsAPI.userGrantReads, userID)
	if f.projectsAPI.userGrantsErr != nil {
		return nil, f.projectsAPI.userGrantsErr
	}
	return slices.Clone(f.projectsAPI.userGrants[userID]), nil
}

// projectDay is a fixed instant for project fixtures.
func projectDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 8, 0, 0, 0, time.UTC)
}

// projectsBoardSample is the Projects board's data: odabank (3 repositories,
// 27 tags), gosse (2, 17), metalab (10, 50) and the empty sandbox.
func projectsBoardSample() []ports.ProjectSummary {
	return []ports.ProjectSummary{
		{Name: "gosse", CreatedAt: projectDay(time.September, 4), Repositories: 2, Tags: 17, LastPushed: projectDay(time.November, 18)},
		{Name: "metalab", CreatedAt: projectDay(time.August, 1), Repositories: 10, Tags: 50, LastPushed: projectDay(time.November, 20)},
		{Name: "odabank", CreatedAt: projectDay(time.November, 2), Repositories: 3, Tags: 27, LastPushed: projectDay(time.November, 19)},
		{Name: "sandbox", CreatedAt: projectDay(time.November, 15)},
	}
}
