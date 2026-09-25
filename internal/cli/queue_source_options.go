package cli

import (
	"encoding/json"
	"fmt"

	"github.com/brettinternet/worklease/internal/config"
)

func queueSourceOptions(source config.QueueSource) map[string]string {
	options := map[string]string{"id": source.ID, "checkout": source.Checkout, "host": source.Host, "repository": source.Repository, "account": source.Account, "allowGitNetwork": fmt.Sprint(source.AllowGitNetwork)}
	if source.Project != nil {
		data, _ := json.Marshal(source.Project)
		options["project"] = string(data)
	}
	return options
}
