package cli

import (
	"encoding/json"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
)

func TestQueueSourceOptionsPreserveProjectBindingJSON(t *testing.T) {
	source := config.QueueSource{
		ID: "issues", Adapter: "github", Host: "github.com", Repository: "org/repo", Account: "tester",
		GitHubProject: &config.QueueProject{Owner: "acme", Number: 5, ID: "PVT_project5", FieldID: "PVTSSF_status", Options: map[string]string{"option-progress": "in-progress"}, AllowWrites: true},
	}
	var project config.QueueProject
	if err := json.Unmarshal([]byte(queueSourceOptions(source)["project"]), &project); err != nil {
		t.Fatal(err)
	}
	if project.Owner != source.GitHubProject.Owner || project.Number != source.GitHubProject.Number || project.ID != source.GitHubProject.ID || project.FieldID != source.GitHubProject.FieldID || project.Options["option-progress"] != "in-progress" || !project.AllowWrites {
		t.Fatalf("source options dropped project binding: %+v", project)
	}
}

func TestQueueSourceOptionsPassBacklogCompleteStatus(t *testing.T) {
	t.Parallel()
	source := config.QueueSource{ID: "tasks", Adapter: "backlog-md", Checkout: "/repo", Workflow: map[string]string{"complete": "Done"}}
	if got := queueSourceOptions(source)["completeStatus"]; got != "Done" {
		t.Fatalf("completeStatus = %q", got)
	}
}
