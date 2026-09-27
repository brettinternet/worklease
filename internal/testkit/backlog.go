package testkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Environment knobs read by the fake Backlog.md process.
const (
	// FakeBacklogListDependencies makes task list rows carry dependencies,
	// like a Backlog.md release that reports bulk edges.
	FakeBacklogListDependencies = "FAKE_BACKLOG_LIST_DEPENDENCIES"
	// FakeBacklogFailViewAfterEdit names a marker file. A successful edit
	// creates it, and task views then fail, so the edit's read-back is lost.
	FakeBacklogFailViewAfterEdit = "FAKE_BACKLOG_FAIL_VIEW_AFTER_EDIT"
)

// FakeBacklog installs a `backlog` executable in a private directory and
// returns that directory. The executable re-runs the test binary, whose
// TestMain must first call RunFakeBacklogIfInvoked.
//
// The fake implements the Backlog.md 1.52 CLI subset that Worklease uses:
// --version, config get, and task list/view/create/edit with JSON output
// shaped like the real CLI's. It keeps each task as a JSON file under the
// project's backlog directory, so edits change task files the way the real
// CLI does. It never commits: an edit in a project with auto_commit enabled
// fails, so tests of commit effects must use the real CLI. Unknown commands
// and flags fail, so a caller cannot silently depend on unmodeled behavior.
func FakeBacklog(t testing.TB) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(executable, filepath.Join(dir, "backlog")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// RunFakeBacklogIfInvoked runs the fake Backlog.md CLI and exits when the
// process was started through FakeBacklog's executable.
func RunFakeBacklogIfInvoked() {
	if filepath.Base(os.Args[0]) != "backlog" {
		return
	}
	output, err := fakeBacklog(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake backlog:", err)
		os.Exit(1)
	}
	fmt.Fprint(os.Stdout, output)
	os.Exit(0)
}

type fakeBacklogCriterion struct {
	Index   int    `json:"index"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

type fakeBacklogComment struct {
	Index  int    `json:"index"`
	Body   string `json:"body"`
	Author string `json:"author,omitempty"`
}

type fakeBacklogTask struct {
	ID                  string                 `json:"id"`
	Title               string                 `json:"title"`
	Status              string                 `json:"status"`
	Assignees           []string               `json:"assignees"`
	Ordinal             int                    `json:"ordinal"`
	UpdatedAt           *string                `json:"updatedAt"`
	Dependencies        []string               `json:"dependencies"`
	AcceptanceCriteria  []fakeBacklogCriterion `json:"acceptanceCriteria"`
	ImplementationNotes *string                `json:"implementationNotes"`
	Comments            []fakeBacklogComment   `json:"comments"`
}

type fakeBacklogProject struct {
	settings map[string]string
	statuses []string
	dir      string // task directory, relative to the project root
}

func loadFakeBacklogProject() (fakeBacklogProject, error) {
	data, err := os.ReadFile("backlog.config.yml")
	if err != nil {
		return fakeBacklogProject{}, err
	}
	project := fakeBacklogProject{settings: map[string]string{}}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		project.settings[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	for _, status := range strings.Split(strings.Trim(project.settings["statuses"], "[]"), ",") {
		if status = strings.TrimSpace(status); status != "" {
			project.statuses = append(project.statuses, status)
		}
	}
	if len(project.statuses) == 0 {
		return project, errors.New("no configured statuses")
	}
	directory := project.settings["backlog_directory"]
	if directory == "" {
		directory = "backlog"
	}
	project.dir = filepath.Join(directory, "tasks")
	return project, nil
}

func (p fakeBacklogProject) tasks() ([]fakeBacklogTask, error) {
	entries, err := os.ReadDir(p.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []fakeBacklogTask{}, nil
	}
	if err != nil {
		return nil, err
	}
	tasks := []fakeBacklogTask{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(p.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var task fakeBacklogTask
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Ordinal < tasks[j].Ordinal })
	return tasks, nil
}

func (p fakeBacklogProject) path(id string) string {
	return filepath.Join(p.dir, strings.ToLower(id)+".json")
}

func (p fakeBacklogProject) save(task fakeBacklogTask) error {
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path(task.ID), data, 0o600)
}

func (p fakeBacklogProject) status(value string) (string, error) {
	for _, status := range p.statuses {
		if strings.EqualFold(status, value) {
			return status, nil
		}
	}
	return "", fmt.Errorf("status %q is not configured", value)
}

// row renders a task like Backlog.md: list rows omit dependencies and details,
// and a task is ready when it is not in the final status and every
// prerequisite is.
func (p fakeBacklogProject) row(task fakeBacklogTask, all []fakeBacklogTask, detail bool) map[string]any {
	statuses := map[string]string{}
	for _, other := range all {
		statuses[other.ID] = other.Status
	}
	blocking, missing := []string{}, []string{}
	for _, dependency := range task.Dependencies {
		status, found := statuses[dependency]
		if !found {
			missing = append(missing, dependency)
		} else if status != p.statuses[len(p.statuses)-1] {
			blocking = append(blocking, dependency)
		}
	}
	ready := task.Status != p.statuses[len(p.statuses)-1] && len(blocking) == 0 && len(missing) == 0
	row := map[string]any{"id": task.ID, "title": task.Title, "status": task.Status, "priority": nil, "assignees": task.Assignees, "labels": []string{}, "parentTaskId": nil, "ordinal": task.Ordinal, "updatedAt": task.UpdatedAt, "isReady": ready}
	if detail || os.Getenv(FakeBacklogListDependencies) != "" {
		row["dependencies"] = task.Dependencies
	}
	if detail {
		row["path"] = p.path(task.ID)
		row["description"] = nil
		row["readiness"] = map[string]any{"isReady": ready, "isBlocked": len(blocking) > 0, "blockingDependencies": blocking, "missingDependencies": missing}
		row["acceptanceCriteria"] = task.AcceptanceCriteria
		row["implementationNotes"] = task.ImplementationNotes
		row["comments"] = task.Comments
	}
	return row
}

func fakeBacklogJSON(kind, field string, value any) (string, error) {
	data, err := json.MarshalIndent(map[string]any{"schemaVersion": 1, "kind": kind, field: value}, "", "  ")
	return string(data) + "\n", err
}

func fakeBacklog(args []string) (string, error) {
	if len(args) == 1 && args[0] == "--version" {
		return "1.52.0\n", nil
	}
	project, err := loadFakeBacklogProject()
	if err != nil {
		return "", err
	}
	switch {
	case len(args) == 3 && args[0] == "config" && args[1] == "get":
		if args[2] == "statuses" {
			return strings.Join(project.statuses, ", ") + "\n", nil
		}
		keys := map[string]string{"remoteOperations": "remote_operations", "checkActiveBranches": "check_active_branches", "autoCommit": "auto_commit", "bypassGitHooks": "bypass_git_hooks"}
		value, ok := project.settings[keys[args[2]]]
		if !ok {
			return "", fmt.Errorf("unmodeled config key %q", args[2])
		}
		return value + "\n", nil
	case len(args) == 3 && args[0] == "task" && args[1] == "list" && args[2] == "--json":
		tasks, err := project.tasks()
		if err != nil {
			return "", err
		}
		rows := make([]map[string]any, 0, len(tasks))
		for _, task := range tasks {
			rows = append(rows, project.row(task, tasks, false))
		}
		return fakeBacklogJSON("task-list", "tasks", rows)
	case len(args) == 4 && args[0] == "task" && args[1] == "view" && args[3] == "--json":
		if marker := os.Getenv(FakeBacklogFailViewAfterEdit); marker != "" {
			if _, err := os.Stat(marker); err == nil {
				return "", errors.New("view failed after edit")
			}
		}
		tasks, err := project.tasks()
		if err != nil {
			return "", err
		}
		for _, task := range tasks {
			if task.ID == args[2] {
				return fakeBacklogJSON("task-view", "task", project.row(task, tasks, true))
			}
		}
		return "", fmt.Errorf("task %s not found", args[2])
	case len(args) >= 3 && args[0] == "task" && args[1] == "create":
		return project.create(args[2], args[3:])
	case len(args) >= 3 && args[0] == "task" && args[1] == "edit":
		return project.edit(args[2], args[3:])
	}
	return "", fmt.Errorf("unmodeled command %q", args)
}

func (p fakeBacklogProject) create(title string, flags []string) (string, error) {
	tasks, err := p.tasks()
	if err != nil {
		return "", err
	}
	task := fakeBacklogTask{ID: "TASK-" + strconv.Itoa(len(tasks)+1), Title: title, Status: p.statuses[0], Assignees: []string{}, Ordinal: 1000 * (len(tasks) + 1), Dependencies: []string{}, AcceptanceCriteria: []fakeBacklogCriterion{}, Comments: []fakeBacklogComment{}}
	for i := 0; i < len(flags); i++ {
		switch flags[i] {
		case "--no-dod-defaults":
			continue
		case "--assignee", "--ac", "--depends-on":
		default:
			return "", fmt.Errorf("unmodeled create flag %q", flags[i])
		}
		if i+1 == len(flags) {
			return "", fmt.Errorf("%s needs a value", flags[i])
		}
		value := flags[i+1]
		switch flags[i] {
		case "--assignee":
			task.Assignees = strings.Split(value, ",")
		case "--ac":
			task.AcceptanceCriteria = append(task.AcceptanceCriteria, fakeBacklogCriterion{Index: len(task.AcceptanceCriteria) + 1, Text: value})
		case "--depends-on":
			task.Dependencies = strings.Split(value, ",")
		}
		i++
	}
	if err := p.save(task); err != nil {
		return "", err
	}
	return "Created task " + task.ID + "\n", nil
}

func (p fakeBacklogProject) edit(id string, flags []string) (string, error) {
	if p.settings["auto_commit"] == "true" {
		return "", errors.New("auto_commit is not modeled; use the real Backlog.md CLI")
	}
	data, err := os.ReadFile(p.path(id))
	if err != nil {
		return "", fmt.Errorf("task %s not found", id)
	}
	var task fakeBacklogTask
	if err := json.Unmarshal(data, &task); err != nil {
		return "", err
	}
	author := ""
	var comments []string
	for i := 0; i < len(flags); i += 2 {
		if i+1 == len(flags) {
			return "", fmt.Errorf("%s needs a value", flags[i])
		}
		value := flags[i+1]
		switch flags[i] {
		case "--status":
			if task.Status, err = p.status(value); err != nil {
				return "", err
			}
		case "--assignee":
			task.Assignees = []string{}
			if value != "" {
				task.Assignees = strings.Split(value, ",")
			}
		case "--depends-on":
			task.Dependencies = strings.Split(value, ",")
		case "--append-notes":
			notes := value
			if task.ImplementationNotes != nil && *task.ImplementationNotes != "" {
				notes = *task.ImplementationNotes + "\n\n" + value
			}
			task.ImplementationNotes = &notes
		case "--comment":
			comments = append(comments, value)
		case "--comment-author":
			author = value
		default:
			return "", fmt.Errorf("unmodeled edit flag %q", flags[i])
		}
	}
	for _, body := range comments {
		task.Comments = append(task.Comments, fakeBacklogComment{Index: len(task.Comments) + 1, Body: body, Author: author})
	}
	// Backlog.md records minute-resolution update times.
	updated := time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339)
	task.UpdatedAt = &updated
	if err := p.save(task); err != nil {
		return "", err
	}
	if marker := os.Getenv(FakeBacklogFailViewAfterEdit); marker != "" {
		if err := os.WriteFile(marker, []byte("edited"), 0o600); err != nil {
			return "", err
		}
	}
	return "Updated task " + id + "\n", nil
}
