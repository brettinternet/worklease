package cli

import (
	"encoding/json"
	"fmt"

	"github.com/brettinternet/worklease/internal/config"
)

// queueSourceOptions passes only the configured scope, never the credential,
// to the adapter. The helper executable resolves its token in the adapter.
func queueSourceOptions(source config.QueueSource) map[string]string {
	options := map[string]string{"id": source.ID, "checkout": source.Checkout, "host": source.Host, "repository": source.Repository, "account": source.Account, "allowGitNetwork": fmt.Sprint(source.AllowGitNetwork)}
	if source.Adapter == "beads" {
		options["completeStatus"] = source.Workflow["complete"]
	}
	if source.Adapter == "linear" {
		options["organization"], options["team"], options["project"] = source.Organization, source.Team, source.Project
		encoded, _ := json.Marshal(source.CredentialHelper)
		options["credentialHelper"] = string(encoded)
	}
	if source.Adapter == "github" && source.GitHubProject != nil {
		data, _ := json.Marshal(source.GitHubProject)
		options["project"] = string(data)
	}
	return options
}
