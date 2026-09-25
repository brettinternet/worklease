package cli

import (
	"encoding/json"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
)

func TestQueueSourceOptionsPreserveProjectBindingJSON(t *testing.T) {
	source := config.QueueSource{
		ID: "issues", Adapter: "github", Host: "github.com", Repository: "org/repo", Account: "tester",
		Project: &config.QueueProject{Owner: "acme", Number: 5, ID: "PVT_project5", FieldID: "PVTSSF_status", Options: map[string]string{"option-progress": "in-progress"}, AllowWrites: true},
	}
	var project config.QueueProject
	if err := json.Unmarshal([]byte(queueSourceOptions(source)["project"]), &project); err != nil {
		t.Fatal(err)
	}
	if project.Owner != source.Project.Owner || project.Number != source.Project.Number || project.ID != source.Project.ID || project.FieldID != source.Project.FieldID || project.Options["option-progress"] != "in-progress" || !project.AllowWrites {
		t.Fatalf("source options dropped project binding: %+v", project)
	}
}
